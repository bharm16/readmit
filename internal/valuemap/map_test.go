package valuemap_test

import (
	"github.com/bharm16/readmit/internal/valuemap"
	"strings"
	"testing"
)

func owned() valuemap.Document {
	return valuemap.Document{Schema: valuemap.Schema, Project: "0123456789abcdef01234567", Name: "Owned application names", Edition: "2.5.1", SourceSelector: "MSH[1]-3[1]", DestinationSelector: "MSH[1]-3[1]", SourceMeaning: "Sender application name", DestinationMeaning: "Recorded receiver application name", Provenance: "Owned interface declaration", Entries: []valuemap.Entry{{Source: "OWNED", Destination: "LOCAL", SourceMeaning: "Owned test sender", DestinationMeaning: "Local test receiver", Provenance: "Owner note"}}, Associations: []valuemap.Association{}}
}
func TestValueMapCSVRoundtripKeepsScopeMeaningsAndLeadingZeroTable(t *testing.T) {
	d := owned()
	d.Table = &valuemap.Table{ID: "0003", Edition: "2.5.1", Provenance: "Explicit table reference"}
	raw, err := valuemap.CSV(d)
	if err != nil {
		t.Fatal(err)
	}
	read, err := valuemap.ImportCSV(raw, d.Project)
	if err != nil || read.Table.ID != "0003" || read.SourceSelector != d.SourceSelector || read.Entries[0] != d.Entries[0] {
		t.Fatalf("lossy CSV: %+v %v", read, err)
	}
	encoded, err := valuemap.Encode(read)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = valuemap.Decode(encoded); err != nil {
		t.Fatal(err)
	}
}
func TestValueMapRejectsDuplicatesMalformedCSVAndScopeBroadeningWithoutPartialDraft(t *testing.T) {
	d := owned()
	raw, _ := valuemap.CSV(d)
	for _, bad := range []string{string(raw) + strings.Split(string(raw), "\n")[1] + "\n", strings.Replace(string(raw), "OWNED,LOCAL", "\"OWNED,LOCAL", 1), strings.Replace(string(raw), "MSH[1]-3[1]", "MSH-3", 1), string(raw) + strings.Replace(strings.Split(string(raw), "\n")[1], "MSH[1]-3[1]", "MSH[1]-4[1]", 1) + "\n"} {
		read, err := valuemap.ImportCSV([]byte(bad), d.Project)
		if err == nil || read.Entries != nil {
			t.Fatalf("invalid CSV published partial draft: %v %+v", err, read)
		}
	}
}
