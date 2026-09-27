package fhirvalidator

// Fixed support roots for the official 6.10.4 validator plus the reviewed R4
// service adapter. Package graph provenance remains in the staged manifest.
var requiredRoots = map[string]string{
	"hl7.fhir.r4.core#4.0.1":          "ebd7731df7d36b5b7d39d5fb6c9d77b44bb7fe5742f1a2e87f164738c3289d44",
	"hl7.fhir.xver-extensions#0.1.0":  "f3bb9fa2083402e88a02b41f433655274e8a1cca563211c8f7ba6fd0badf537a",
	"hl7.terminology.r4#6.2.0":        "79404c9cc95491fc0155627cd039c401a6eb4748175328131e91b709a41300e2",
	"hl7.fhir.uv.extensions.r4#5.2.0": "b406e75575f05676559d0759770c5939d023ee72fb2ef38e0b3259328487720a",
}

// The one qualified validator and runtime combination.
const (
	validatorVersion = "6.10.4"
	validatorSHA256  = "1106b9d58f9e363e47bea7c4fc065841e5fc91fe9d062775c3bfdd212bd653cc"
	runtimeVersion   = "21.0.12.1+1"
	runtimeSHA256    = "14be1f35ebdbd1f6e8d57eb911a3ffb74d6d9aa255abc5daf2b1302002cf2cf2"
)
