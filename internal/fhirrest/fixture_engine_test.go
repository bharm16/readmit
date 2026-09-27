package fhirrest_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func oo(code int) (int, map[string]string, []byte) {
	return code, map[string]string{}, []byte(`{"resourceType":"OperationOutcome","issue":[{"severity":"error","code":"processing"}]}`)
}
func (f *server) matching(query string) []string {
	values, _ := url.ParseQuery(query)
	ids := []string{}
	for id, raw := range f.resources {
		var resource struct {
			Identifier []struct{ System, Value string }
		}
		json.Unmarshal(raw, &resource)
		for _, v := range resource.Identifier {
			if values.Get("identifier") == v.System+"|"+v.Value {
				ids = append(ids, id)
				break
			}
		}
	}
	return ids
}
func (f *server) respond(method, address string, headers http.Header, body []byte) (int, map[string]string, []byte) {
	uri, _ := url.Parse(address)
	parts := strings.Split(uri.Path, "/")
	id := ""
	if len(parts) > 1 {
		id = parts[1]
	}
	if method == "POST" && (address == "/fhir" || address == "" || address == f.s.URL+"/fhir") {
		var bundle struct {
			ResourceType string `json:"resourceType"`
			Type         string `json:"type"`
			Entry        []struct {
				Resource json.RawMessage                                    `json:"resource"`
				Request  struct{ Method, URL, IfMatch, IfNoneExist string } `json:"request"`
			} `json:"entry"`
		}
		if json.Unmarshal(body, &bundle) != nil || bundle.ResourceType != "Bundle" {
			return oo(400)
		}
		previous := map[string][]byte{}
		versions := map[string]int{}
		for k, v := range f.resources {
			previous[k] = append([]byte(nil), v...)
			versions[k] = f.versions[k]
		}
		oldCreates := f.creates
		entries := []any{}
		for _, entry := range bundle.Entry {
			h := http.Header{}
			h.Set("If-Match", entry.Request.IfMatch)
			h.Set("If-None-Exist", entry.Request.IfNoneExist)
			code, metadata, payload := f.respond(entry.Request.Method, entry.Request.URL, h, entry.Resource)
			if code >= 400 && bundle.Type == "transaction" {
				f.resources = previous
				f.versions = versions
				f.creates = oldCreates
				return oo(code)
			}
			e := map[string]any{"response": map[string]any{"status": strconv.Itoa(code)}}
			response := e["response"].(map[string]any)
			if v := metadata["Location"]; v != "" {
				response["location"] = v
			}
			if v := metadata["ETag"]; v != "" {
				response["etag"] = v
			}
			if len(payload) > 0 {
				e["resource"] = json.RawMessage(payload)
			}
			entries = append(entries, e)
		}
		raw, _ := json.Marshal(map[string]any{"resourceType": "Bundle", "type": bundle.Type + "-response", "entry": entries})
		return 200, map[string]string{}, raw
	}
	if parts[0] != "Patient" {
		return oo(404)
	}
	switch method {
	case "GET":
		raw, ok := f.resources[id]
		version := f.versions[id]
		if len(parts) == 4 && parts[2] == "_history" {
			version, _ = strconv.Atoi(parts[3])
			raw, ok = f.history[id][version]
		}
		if !ok {
			return oo(404)
		}
		tag := fmt.Sprintf(`W/"%d"`, version)
		if headers.Get("If-None-Match") == tag {
			return 304, map[string]string{"ETag": tag}, nil
		}
		return 200, map[string]string{"ETag": tag}, raw
	case "POST", "PUT":
		code := 200
		if method == "POST" {
			if query := headers.Get("If-None-Exist"); query != "" {
				matches := f.matching(query)
				if len(matches) > 1 {
					return oo(412)
				}
				if len(matches) == 1 {
					return 200, map[string]string{"Location": f.s.URL + "/fhir/Patient/" + matches[0]}, f.resources[matches[0]]
				}
			}
			id = ""
		}
		if method == "PUT" && id == "" {
			matches := f.matching(uri.RawQuery)
			if len(matches) > 1 {
				return oo(412)
			}
			if len(matches) == 1 {
				id = matches[0]
			}
		}
		if id == "" {
			f.creates++
			id = strconv.Itoa(f.creates)
			code = 201
		} else if _, ok := f.resources[id]; !ok {
			code = 201
		}
		if match := headers.Get("If-Match"); match != "" && match != fmt.Sprintf(`W/"%d"`, f.versions[id]) {
			return oo(412)
		}
		var value map[string]any
		if json.Unmarshal(body, &value) != nil {
			return oo(400)
		}
		f.versions[id]++
		value["id"] = id
		value["meta"] = map[string]any{"versionId": strconv.Itoa(f.versions[id])}
		f.resources[id], _ = json.Marshal(value)
		if f.history[id] == nil {
			f.history[id] = map[int][]byte{}
		}
		f.history[id][f.versions[id]] = append([]byte(nil), f.resources[id]...)
		return code, map[string]string{"Location": f.s.URL + "/fhir/Patient/" + id + "/_history/" + strconv.Itoa(f.versions[id]), "ETag": fmt.Sprintf(`W/"%d"`, f.versions[id])}, f.resources[id]
	case "PATCH":
		raw, ok := f.resources[id]
		if !ok {
			return oo(404)
		}
		if headers.Get("If-Match") != fmt.Sprintf(`W/"%d"`, f.versions[id]) {
			return oo(412)
		}
		var patch []struct {
			Op, Path string
			Value    any
		}
		if json.Unmarshal(body, &patch) != nil {
			return oo(400)
		}
		var value map[string]any
		json.Unmarshal(raw, &value)
		for _, op := range patch {
			if op.Op != "replace" || op.Path != "/active" {
				return oo(422)
			}
			value["active"] = op.Value
		}
		f.versions[id]++
		value["meta"] = map[string]any{"versionId": strconv.Itoa(f.versions[id])}
		f.resources[id], _ = json.Marshal(value)
		if f.history[id] == nil {
			f.history[id] = map[int][]byte{}
		}
		f.history[id][f.versions[id]] = append([]byte(nil), f.resources[id]...)
		return 200, map[string]string{"ETag": fmt.Sprintf(`W/"%d"`, f.versions[id])}, f.resources[id]
	case "DELETE":
		delete(f.resources, id)
		delete(f.versions, id)
		return 204, map[string]string{}, nil
	}
	return oo(405)
}
