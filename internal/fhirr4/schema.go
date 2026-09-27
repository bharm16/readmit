package fhirr4

import "strings"

// The finite projection vocabulary is hand-authored from fixed R4 4.0.1 types.
// It is not a StructureDefinition engine or an implementation-guide validator.
type field struct {
	typ            string
	many, required bool
	system, choice string
}

func one(t string) field       { return field{typ: t} }
func many(t string) field      { return field{typ: t, many: true} }
func required(t string) field  { return field{typ: t, required: true} }
func code(system string) field { return field{typ: "code", system: system} }
func merge(a, b map[string]field) map[string]field {
	out := map[string]field{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

var baseResource = map[string]field{
	"resourceType":  required("code"),
	"id":            one("id"),
	"meta":          one("Meta"),
	"implicitRules": one("uri"),
	"language":      one("code"),
}
var baseElement = map[string]field{
	"id":        one("string"),
	"extension": many("Extension"),
}
var backbone = merge(baseElement, map[string]field{
	"modifierExtension": many("Extension"),
})
var domainResource = merge(baseResource, map[string]field{
	"text":              one("Narrative"),
	"contained":         many("Resource"),
	"extension":         many("Extension"),
	"modifierExtension": many("Extension"),
})
var definitions = func() map[string]map[string]field {
	d := map[string]map[string]field{
		"PrimitiveMetadata": baseElement,

		"Meta": merge(baseElement,
			map[string]field{"versionId": one("id"), "lastUpdated": one("instant"), "source": one("uri"), "profile": many("canonical"), "security": many("Coding"), "tag": many("Coding")}),

		"Narrative": merge(baseElement,
			map[string]field{"status": required("code"), "div": required("xhtml")}),

		"Identifier": merge(baseElement,
			map[string]field{"use": one("code"), "type": one("CodeableConcept"), "system": one("uri"), "value": one("string"), "period": one("Period"), "assigner": one("Reference")}),

		"Coding": merge(baseElement,
			map[string]field{"system": one("uri"), "version": one("string"), "code": one("code"), "display": one("string"), "userSelected": one("boolean")}),

		"CodeableConcept": merge(baseElement,
			map[string]field{"coding": many("Coding"), "text": one("string")}),

		"Quantity": merge(baseElement,
			map[string]field{"value": one("decimal"), "comparator": one("code"), "unit": one("string"), "system": one("uri"), "code": one("code")}),

		"Range": merge(baseElement,
			map[string]field{"low": one("Quantity"), "high": one("Quantity")}),

		"Ratio": merge(baseElement,
			map[string]field{"numerator": one("Quantity"), "denominator": one("Quantity")}),

		"Period": merge(baseElement,
			map[string]field{"start": one("dateTime"), "end": one("dateTime")}),

		"Reference": merge(baseElement,
			map[string]field{"reference": one("string"), "type": one("uri"), "identifier": one("Identifier"), "display": one("string")}),

		"HumanName": merge(baseElement,
			map[string]field{"use": one("code"), "text": one("string"), "family": one("string"), "given": many("string"), "prefix": many("string"), "suffix": many("string"), "period": one("Period")}),

		"Address": merge(baseElement,
			map[string]field{"use": one("code"), "type": one("code"), "text": one("string"), "line": many("string"), "city": one("string"), "district": one("string"), "state": one("string"), "postalCode": one("string"), "country": one("string"), "period": one("Period")}),

		"ContactPoint": merge(baseElement,
			map[string]field{"system": one("code"), "value": one("string"), "use": one("code"), "rank": one("positiveInt"), "period": one("Period")}),

		"Attachment": merge(baseElement,
			map[string]field{"contentType": one("code"), "language": one("code"), "data": one("base64Binary"), "url": one("url"), "size": one("unsignedInt"), "hash": one("base64Binary"), "title": one("string"), "creation": one("dateTime")}),

		"Annotation": merge(baseElement,
			map[string]field{"time": one("dateTime"), "text": required("markdown")}),

		"Extension": merge(baseElement,
			map[string]field{"url": required("uri")}),

		"Patient": merge(domainResource,
			map[string]field{"identifier": many("Identifier"), "active": one("boolean"), "name": many("HumanName"), "telecom": many("ContactPoint"), "gender": code("http://hl7.org/fhir/administrative-gender"), "birthDate": one("date"), "address": many("Address"), "maritalStatus": one("CodeableConcept"), "photo": many("Attachment"), "generalPractitioner": many("Reference"), "managingOrganization": one("Reference"), "link": many("PatientLink"), "communication": many("Communication"), "contact": many("PatientContact")}),

		"PatientLink": merge(backbone,
			map[string]field{"other": required("Reference"), "type": required("code")}),

		"Communication": merge(backbone,
			map[string]field{"language": required("CodeableConcept"), "preferred": one("boolean")}),

		"PatientContact": merge(backbone,
			map[string]field{"relationship": many("CodeableConcept"), "name": one("HumanName"), "telecom": many("ContactPoint"), "address": one("Address"), "gender": one("code"), "organization": one("Reference"), "period": one("Period")}),

		"Practitioner": merge(domainResource,
			map[string]field{"identifier": many("Identifier"), "active": one("boolean"), "name": many("HumanName"), "telecom": many("ContactPoint"), "address": many("Address"), "gender": one("code"), "birthDate": one("date"), "photo": many("Attachment"), "qualification": many("Qualification"), "communication": many("CodeableConcept")}),

		"Qualification": merge(backbone,
			map[string]field{"identifier": many("Identifier"), "code": required("CodeableConcept"), "period": one("Period"), "issuer": one("Reference")}),

		"Location": merge(domainResource,
			map[string]field{"identifier": many("Identifier"), "status": code("http://hl7.org/fhir/location-status"), "operationalStatus": one("Coding"), "name": one("string"), "alias": many("string"), "description": one("string"), "mode": one("code"), "type": many("CodeableConcept"), "telecom": many("ContactPoint"), "address": one("Address"), "physicalType": one("CodeableConcept"), "position": one("Position"), "managingOrganization": one("Reference"), "partOf": one("Reference"), "hoursOfOperation": many("Hours"), "availabilityExceptions": one("string"), "endpoint": many("Reference")}),

		"Position": merge(backbone,
			map[string]field{"longitude": required("decimal"), "latitude": required("decimal"), "altitude": one("decimal")}),

		"Hours": merge(backbone,
			map[string]field{"daysOfWeek": many("code"), "allDay": one("boolean"), "openingTime": one("time"), "closingTime": one("time")}),

		"Appointment": merge(domainResource,
			map[string]field{"identifier": many("Identifier"), "status": required("code"), "cancelationReason": one("CodeableConcept"), "serviceCategory": many("CodeableConcept"), "serviceType": many("CodeableConcept"), "specialty": many("CodeableConcept"), "appointmentType": one("CodeableConcept"), "reasonCode": many("CodeableConcept"), "reasonReference": many("Reference"), "priority": one("unsignedInt"), "description": one("string"), "supportingInformation": many("Reference"), "start": one("instant"), "end": one("instant"), "minutesDuration": one("positiveInt"), "slot": many("Reference"), "created": one("dateTime"), "comment": one("string"), "patientInstruction": one("string"), "basedOn": many("Reference"), "participant": {typ: "Participant", many: true, required: true}, "requestedPeriod": many("Period")}),

		"Participant": merge(backbone,
			map[string]field{"type": many("CodeableConcept"), "actor": one("Reference"), "required": one("code"), "status": required("code"), "period": one("Period")}),

		"Encounter": merge(domainResource,
			map[string]field{"identifier": many("Identifier"), "status": required("code"), "class": required("Coding"), "type": many("CodeableConcept"), "serviceType": one("CodeableConcept"), "priority": one("CodeableConcept"), "subject": one("Reference"), "episodeOfCare": many("Reference"), "basedOn": many("Reference"), "participant": many("EncounterParticipant"), "appointment": many("Reference"), "period": one("Period"), "length": one("Quantity"), "reasonCode": many("CodeableConcept"), "reasonReference": many("Reference"), "diagnosis": many("Diagnosis"), "account": many("Reference"), "location": many("EncounterLocation"), "serviceProvider": one("Reference"), "partOf": one("Reference")}),

		"EncounterParticipant": merge(backbone,
			map[string]field{"type": many("CodeableConcept"), "period": one("Period"), "individual": one("Reference")}),

		"Diagnosis": merge(backbone,
			map[string]field{"condition": required("Reference"), "use": one("CodeableConcept"), "rank": one("positiveInt")}),

		"EncounterLocation": merge(backbone,
			map[string]field{"location": required("Reference"), "status": one("code"), "physicalType": one("CodeableConcept"), "period": one("Period")}),

		"ServiceRequest": merge(domainResource,
			map[string]field{"identifier": many("Identifier"), "instantiatesCanonical": many("canonical"), "instantiatesUri": many("uri"), "basedOn": many("Reference"), "replaces": many("Reference"), "requisition": one("Identifier"), "status": required("code"), "intent": required("code"), "category": many("CodeableConcept"), "priority": one("code"), "doNotPerform": one("boolean"), "code": one("CodeableConcept"), "orderDetail": many("CodeableConcept"), "subject": required("Reference"), "encounter": one("Reference"), "asNeededBoolean": {typ: "boolean", choice: "asNeeded"}, "asNeededCodeableConcept": {typ: "CodeableConcept", choice: "asNeeded"}, "authoredOn": one("dateTime"), "requester": one("Reference"), "performerType": one("CodeableConcept"), "performer": many("Reference"), "locationCode": many("CodeableConcept"), "locationReference": many("Reference"), "reasonCode": many("CodeableConcept"), "reasonReference": many("Reference"), "insurance": many("Reference"), "supportingInfo": many("Reference"), "specimen": many("Reference"), "bodySite": many("CodeableConcept"), "note": many("Annotation"), "patientInstruction": one("string"), "relevantHistory": many("Reference")}),

		"Observation": merge(domainResource,
			map[string]field{"identifier": many("Identifier"), "basedOn": many("Reference"), "partOf": many("Reference"), "status": required("code"), "category": many("CodeableConcept"), "code": required("CodeableConcept"), "subject": one("Reference"), "focus": many("Reference"), "encounter": one("Reference"), "issued": one("instant"), "performer": many("Reference"), "dataAbsentReason": one("CodeableConcept"), "interpretation": many("CodeableConcept"), "note": many("Annotation"), "bodySite": one("CodeableConcept"), "method": one("CodeableConcept"), "specimen": one("Reference"), "device": one("Reference"), "referenceRange": many("ReferenceRange"), "hasMember": many("Reference"), "derivedFrom": many("Reference"), "component": many("ObservationComponent")}),

		"ObservationComponent": merge(backbone,
			map[string]field{"code": required("CodeableConcept"), "dataAbsentReason": one("CodeableConcept"), "interpretation": many("CodeableConcept"), "referenceRange": many("ReferenceRange")}),

		"ReferenceRange": merge(backbone,
			map[string]field{"low": one("Quantity"), "high": one("Quantity"), "type": one("CodeableConcept"), "appliesTo": many("CodeableConcept"), "age": one("Range"), "text": one("string")}),

		"DiagnosticReport": merge(domainResource,
			map[string]field{"identifier": many("Identifier"), "basedOn": many("Reference"), "status": required("code"), "category": many("CodeableConcept"), "code": required("CodeableConcept"), "subject": one("Reference"), "encounter": one("Reference"), "issued": one("instant"), "performer": many("Reference"), "resultsInterpreter": many("Reference"), "specimen": many("Reference"), "result": many("Reference"), "imagingStudy": many("Reference"), "media": many("ReportMedia"), "conclusion": one("string"), "conclusionCode": many("CodeableConcept"), "presentedForm": many("Attachment")}),

		"ReportMedia": merge(backbone,
			map[string]field{"comment": one("string"), "link": required("Reference")}),

		"Bundle": merge(baseResource,
			map[string]field{"identifier": one("Identifier"), "type": required("code"), "timestamp": one("instant"), "total": one("unsignedInt"), "link": many("BundleLink"), "entry": many("BundleEntry"), "signature": one("Signature")}),

		"BundleLink": merge(backbone,
			map[string]field{"relation": required("string"), "url": required("uri")}),

		"BundleEntry": merge(backbone,
			map[string]field{"link": many("BundleLink"), "fullUrl": one("uri"), "resource": one("Resource"), "search": one("BundleSearch"), "request": one("BundleRequest"), "response": one("BundleResponse")}),

		"BundleSearch": merge(backbone,
			map[string]field{"mode": one("code"), "score": one("decimal")}),

		"BundleRequest": merge(backbone,
			map[string]field{"method": required("code"), "url": required("uri"), "ifNoneMatch": one("string"), "ifModifiedSince": one("instant"), "ifMatch": one("string"), "ifNoneExist": one("string")}),

		"BundleResponse": merge(backbone,
			map[string]field{"status": required("string"), "location": one("uri"), "etag": one("string"), "lastModified": one("instant"), "outcome": one("Resource")}),

		"Signature": merge(baseElement,
			map[string]field{"type": {typ: "Coding", many: true, required: true}, "when": required("instant"), "who": required("Reference"), "onBehalfOf": one("Reference"), "targetFormat": one("code"), "sigFormat": one("code"), "data": one("base64Binary")}),

		"OperationOutcome": merge(domainResource,
			map[string]field{"issue": {typ: "OutcomeIssue", many: true, required: true}}),

		"OutcomeIssue": merge(backbone,
			map[string]field{"severity": required("code"), "code": required("code"), "details": one("CodeableConcept"), "diagnostics": one("string"), "location": many("string"), "expression": many("string")}),
	}
	choices := func(typ, prefix string, alternatives ...string) {
		for _, t := range alternatives {
			d[typ][prefix+strings.ToUpper(t[:1])+t[1:]] = field{typ: t, choice: prefix}
		}
	}
	choices("Patient", "deceased", "boolean", "dateTime")
	choices("Patient", "multipleBirth", "boolean", "integer")
	choices("Annotation", "author", "Reference", "string")
	d["Timing"] = merge(backbone, map[string]field{
		"event":  many("dateTime"),
		"repeat": one("TimingRepeat"),
		"code":   one("CodeableConcept"),
	})
	d["TimingRepeat"] = merge(baseElement, map[string]field{
		"count":        one("positiveInt"),
		"countMax":     one("positiveInt"),
		"duration":     one("decimal"),
		"durationMax":  one("decimal"),
		"durationUnit": one("code"),
		"frequency":    one("positiveInt"),
		"frequencyMax": one("positiveInt"),
		"period":       one("decimal"),
		"periodMax":    one("decimal"),
		"periodUnit":   one("code"),
		"dayOfWeek":    many("code"),
		"timeOfDay":    many("time"),
		"when":         many("code"),
		"offset":       one("unsignedInt"),
	})
	choices("TimingRepeat", "bounds", "Duration", "Range", "Period")
	d["SampledData"] = merge(baseElement, map[string]field{
		"origin":     required("Quantity"),
		"period":     required("decimal"),
		"factor":     one("decimal"),
		"lowerLimit": one("decimal"),
		"upperLimit": one("decimal"),
		"dimensions": required("positiveInt"),
		"data":       one("string"),
	})
	for _, typ := range []string{"Age", "Count", "Distance", "Duration"} {
		d[typ] = merge(nil, d["Quantity"])
	}
	d["Money"] = merge(baseElement, map[string]field{
		"value":    one("decimal"),
		"currency": one("code"),
	})
	d["Encounter"]["length"] = one("Duration")
	d["Encounter"]["statusHistory"] = many("StatusHistory")
	d["Encounter"]["classHistory"] = many("ClassHistory")
	d["Encounter"]["hospitalization"] = one("Hospitalization")
	d["StatusHistory"] = merge(backbone, map[string]field{
		"status": required("code"),
		"period": required("Period"),
	})
	d["ClassHistory"] = merge(backbone, map[string]field{
		"class":  required("Coding"),
		"period": required("Period"),
	})
	d["Hospitalization"] = merge(backbone, map[string]field{
		"preAdmissionIdentifier": one("Identifier"),
		"origin":                 one("Reference"),
		"admitSource":            one("CodeableConcept"),
		"reAdmission":            one("CodeableConcept"),
		"dietPreference":         many("CodeableConcept"),
		"specialCourtesy":        many("CodeableConcept"),
		"specialArrangement":     many("CodeableConcept"),
		"destination":            one("Reference"),
		"dischargeDisposition":   one("CodeableConcept"),
	})
	choices("ServiceRequest", "occurrence", "dateTime", "Period", "Timing")
	choices("ServiceRequest", "quantity", "Quantity", "Ratio", "Range")
	choices("Observation", "effective", "dateTime", "Period", "Timing", "instant")
	choices("DiagnosticReport", "effective", "dateTime", "Period")
	for _, typ := range []string{"Observation", "ObservationComponent"} {
		choices(typ, "value", "Quantity", "CodeableConcept", "string", "boolean", "integer", "Range", "Ratio", "SampledData", "time", "dateTime", "Period")
	}
	choices("Extension", "value", "base64Binary", "boolean", "canonical", "code", "date", "dateTime", "decimal", "id", "instant", "integer", "markdown", "oid", "positiveInt", "string", "time", "unsignedInt", "uri", "url", "uuid", "Address", "Age", "Annotation", "Attachment", "CodeableConcept", "Coding", "ContactPoint", "Count", "Distance", "Duration", "HumanName", "Identifier", "Money", "Period", "Quantity", "Range", "Ratio", "Reference", "SampledData", "Signature", "Timing", "ContactDetail", "Contributor", "DataRequirement", "Expression", "ParameterDefinition", "RelatedArtifact", "TriggerDefinition", "UsageContext", "Dosage", "Meta")
	return d
}()

func primitive(typ string) bool {
	switch typ {
	case "base64Binary", "boolean", "canonical", "code", "date", "dateTime", "decimal", "id", "instant", "integer", "markdown", "oid", "positiveInt", "string", "time", "unsignedInt", "uri", "url", "uuid", "xhtml":
		return true
	}
	return false
}
func supported(typ string) bool {
	switch typ {
	case "Patient", "Encounter", "Appointment", "Practitioner", "Location", "ServiceRequest", "Observation", "DiagnosticReport", "Bundle", "CapabilityStatement", "OperationOutcome":
		return true
	}
	return false
}

var knownCodes = map[string][]string{
	"Patient.gender":          {"male", "female", "other", "unknown"},
	"Practitioner.gender":     {"male", "female", "other", "unknown"},
	"Appointment.status":      {"proposed", "pending", "booked", "arrived", "fulfilled", "cancelled", "noshow", "entered-in-error", "checked-in", "waitlist"},
	"Participant.status":      {"accepted", "declined", "tentative", "needs-action"},
	"Participant.required":    {"required", "optional", "information-only"},
	"Encounter.status":        {"planned", "arrived", "triaged", "in-progress", "onleave", "finished", "cancelled", "entered-in-error", "unknown"},
	"Location.status":         {"active", "suspended", "inactive"},
	"Observation.status":      {"registered", "preliminary", "final", "amended", "corrected", "cancelled", "entered-in-error", "unknown"},
	"DiagnosticReport.status": {"registered", "partial", "preliminary", "final", "amended", "corrected", "appended", "cancelled", "entered-in-error", "unknown"},
	"ServiceRequest.status":   {"draft", "active", "on-hold", "revoked", "completed", "entered-in-error", "unknown"},
	"ServiceRequest.intent":   {"proposal", "plan", "directive", "order", "original-order", "reflex-order", "filler-order", "instance-order", "option"},
	"OutcomeIssue.severity":   {"fatal", "error", "warning", "information"},
}
