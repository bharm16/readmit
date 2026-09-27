package fhirvalidator

// Policy names the one reading of worker outcomes a result was interpreted
// under. A later validator or a revised reading adds a policy beside this one;
// a retained result is always reread under the policy it names.
const Policy = "readmit-fhir-validation-policy/hl7-validator-6.10.4/v1"

// uncheckedMessages lists the message IDs of the pinned validator 6.10.4
// (the keys of Messages.properties in validator_cli.jar, without a plural
// suffix) whose issue says a check was not performed, with what it leaves
// unevaluated. Every other message, including a constraint key, is a finding
// the validator decided. Listing a message that also reports a real defect
// makes a result undecided, never a pass; omitting one could make a pass.
var uncheckedMessages = map[string]string{
	// Terminology the staged capability cannot answer.
	"Attempt_to_use_Terminology_server_when_no_Terminology_server_is_available": "terminology",
	"CODESYSTEM_TOO_COSTLY_TIME":                                    "terminology",
	"Error_expanding_ValueSet_running_without_terminology_services": "terminology",
	"Error_validating_code_running_without_terminology_services":    "terminology",
	"NO_VALID_DISPLAY_AT_ALL":                                       "terminology",
	"SUBSUMPTION_CS_NOT_FOUND":                                      "terminology",
	"SUBSUMPTION_CS_NOT_FOUND_VERSION":                              "terminology",
	"TERMINOLOGY_TX_NOSVC_BOUND_EXT":                                "terminology",
	"TERMINOLOGY_TX_NOSVC_BOUND_REQ":                                "terminology",
	"TERMINOLOGY_TX_SYSTEM_NOT_USABLE":                              "terminology",
	"TERMINOLOGY_TX_SYSTEM_UNSUPPORTED":                             "terminology",
	"TERMINOLOGY_TX_UNKNOWN_OID":                                    "terminology",
	"Terminology_TX_Binding_CantCheck":                              "terminology",
	"Terminology_TX_Binding_NoServer":                               "terminology",
	"Terminology_TX_Binding_NoSource":                               "terminology",
	"Terminology_TX_NoValid_15":                                     "terminology",
	"Terminology_TX_NoValid_9":                                      "terminology",
	"Terminology_TX_System_Unknown":                                 "terminology",
	"Terminology_TX_ValueSet_NotFound":                              "terminology",
	"TYPE_SPECIFIC_CHECKS_DT_QTY_MAX_NO_UCUM_SVC":                   "terminology",
	"TYPE_SPECIFIC_CHECKS_DT_QTY_MIN_NO_UCUM_SVC":                   "terminology",
	"UNABLE_TO_CHECK_IF_THE_PROVIDED_CODES_ARE_IN_THE_VALUE_SET_":   "terminology",
	"UNABLE_TO_CHECK_IF_THE_PROVIDED_CODES_ARE_IN_THE_VALUE_SET_CS": "terminology",
	"UNABLE_TO_CHECK_IF_THE_PROVIDED_CODES_ARE_IN_THE_VALUE_SET_VS": "terminology",
	"UNKNOWN_CODESYSTEM":                                            "terminology",
	"UNKNOWN_CODESYSTEM_CODING_NOT_CHECKED":                         "terminology",
	"UNKNOWN_CODESYSTEM_EXP":                                        "terminology",
	"UNKNOWN_CODESYSTEM_VERSION":                                    "terminology",
	"UNKNOWN_CODESYSTEM_VERSION_EXP":                                "terminology",
	"UNKNOWN_CODESYSTEM_VERSION_EXP_NONE":                           "terminology",
	"UNKNOWN_CODESYSTEM_VERSION_NONE":                               "terminology",
	"UNKNOWN_CODESYSTEM_VERSION_UNK":                                "terminology",
	"UNKNOWN_CODE_IN_FRAGMENT":                                      "terminology",
	"UNSUPPORTED_ECL":                                               "terminology",
	"Unable_to_connect_to_terminology_server":                       "terminology",
	"Unable_to_connect_to_terminology_server_Use_parameter_tx_na_tun_run_without_using_terminology_services_to_validate_LOINC_SNOMED_ICDX_etc_Error__": "terminology",
	"Unable_to_resolve_slice_matching__slice_matching_by_value_set_not_done":                                                                           "terminology",
	"Unable_to_resolve_system__value_set_has_include_with_unknown_system":                                                                              "terminology",
	"Unable_to_resolve_value_Set_":                 "terminology",
	"Unable_to_validate_code_without_using_server": "terminology",
	"VALUESET_TOO_COSTLY_TIME":                     "terminology",
	"VALUESET_UNC_SYSTEM_WARNING":                  "terminology",
	"VALUESET_UNC_SYSTEM_WARNING_VER":              "terminology",
	"VS_EXP_FILTER_UNK":                            "terminology",
	"VS_EXP_IMPORT_NULL":                           "terminology",
	"VS_EXP_IMPORT_NULL_X":                         "terminology",
	"VS_EXP_IMPORT_UNK":                            "terminology",
	"VS_EXP_IMPORT_UNK_PINNED":                     "terminology",
	"VS_EXP_IMPORT_UNK_PINNED_X":                   "terminology",
	"VS_EXP_IMPORT_UNK_X":                          "terminology",

	// Invariants the validator could not evaluate.
	"ED_INVARIANT_NO_KEY":                               "invariants",
	"FHIRPATH_HO_HOST_SERVICES":                         "invariants",
	"FHIRPATH_NOT_IMPLEMENTED":                          "invariants",
	"Problem_processing_expression__in_profile__path__": "invariants",

	// Profiles, extensions and definitions the staged packages do not hold.
	"CANONICAL_MULTIPLE_VERSIONS_KNOWN":                       "profiles",
	"EXTENSION_CONTEXT_UNABLE_TO_CHECK_PROFILE":               "profiles",
	"EXTENSION_CONTEXT_UNABLE_TO_FIND_PROFILE":                "profiles",
	"Extension_EXT_Unknown":                                   "profiles",
	"Extension_EXT_Unknown_NotHere":                           "profiles",
	"FHIRPATH_RESOLVE_DISCRIMINATOR_CANT_FIND":                "profiles",
	"Profile___base__could_not_be_resolved":                   "profiles",
	"Reference_REF_CantResolveProfile":                        "profiles",
	"Unable_to_find_base__for_":                               "profiles",
	"Unable_to_find_profile__at_":                             "profiles",
	"Unable_to_resolve_profile__in_element_":                  "profiles",
	"VALIDATION_VAL_GLOBAL_PROFILE_UNKNOWN":                   "profiles",
	"VALIDATION_VAL_PROFILE_DEPENDS_NOT_RESOLVED":             "profiles",
	"VALIDATION_VAL_PROFILE_UNKNOWN_ERROR":                    "profiles",
	"VALIDATION_VAL_PROFILE_UNKNOWN_ERROR_NETWORK":            "profiles",
	"VALIDATION_VAL_PROFILE_UNKNOWN_NOT_POLICY":               "profiles",
	"Validation_VAL_Profile_Unknown":                          "profiles",
	"Validation_VAL_Unknown_Profile":                          "profiles",
	"_has_children__for_type__in_profile__but_cant_find_type": "profiles",

	// Structural checks inside an available profile that were not performed.
	"Problem_evaluating_slicing_expression_for_element_in_profile__path__fhirPath___": "structure",
	"REQUEST_TOO_COSTLY_TIME":                                                "structure",
	"TYPE_SPECIFIC_CHECKS_DT_CANONICAL_RESOLVE":                              "structure",
	"TYPE_SPECIFIC_CHECKS_DT_CANONICAL_RESOLVE_NC":                           "structure",
	"TYPE_SPECIFIC_CHECKS_DT_QTY_MAX_CODE_MISMATCH":                          "structure",
	"TYPE_SPECIFIC_CHECKS_DT_QTY_MAX_MIN_NO_CODE":                            "structure",
	"TYPE_SPECIFIC_CHECKS_DT_QTY_MAX_MIN_NO_SYSTEM":                          "structure",
	"TYPE_SPECIFIC_CHECKS_DT_QTY_MAX_MIN_NO_VALUE":                           "structure",
	"TYPE_SPECIFIC_CHECKS_DT_QTY_MAX_SYSTEM_MISMATCH":                        "structure",
	"TYPE_SPECIFIC_CHECKS_DT_QTY_MAX_VALUE_NO_CODE":                          "structure",
	"TYPE_SPECIFIC_CHECKS_DT_QTY_MAX_VALUE_NO_SYSTEM":                        "structure",
	"TYPE_SPECIFIC_CHECKS_DT_QTY_MAX_VALUE_NO_VALUE":                         "structure",
	"TYPE_SPECIFIC_CHECKS_DT_QTY_MIN_CODE_MISMATCH":                          "structure",
	"TYPE_SPECIFIC_CHECKS_DT_QTY_MIN_MIN_NO_CODE":                            "structure",
	"TYPE_SPECIFIC_CHECKS_DT_QTY_MIN_MIN_NO_SYSTEM":                          "structure",
	"TYPE_SPECIFIC_CHECKS_DT_QTY_MIN_MIN_NO_VALUE":                           "structure",
	"TYPE_SPECIFIC_CHECKS_DT_QTY_MIN_SYSTEM_MISMATCH":                        "structure",
	"TYPE_SPECIFIC_CHECKS_DT_QTY_MIN_VALUE_NO_CODE":                          "structure",
	"TYPE_SPECIFIC_CHECKS_DT_QTY_MIN_VALUE_NO_SYSTEM":                        "structure",
	"TYPE_SPECIFIC_CHECKS_DT_QTY_MIN_VALUE_NO_VALUE":                         "structure",
	"UNSUPPORTED_SLICING_COMPLEXITY":                                         "structure",
	"Unable_to_resolve_slice_matching__no_fixed_value_or_required_value_set": "structure",
	"Validation_VAL_Profile_NoCheckMax":                                      "structure",
	"Validation_VAL_Profile_NoCheckMin":                                      "structure",
}

// uncheckedCodes are OperationOutcome issue types that mean processing did
// not complete, whatever message carried them.
var uncheckedCodes = map[string]bool{"not-supported": true, "not-found": true, "too-costly": true, "incomplete": true, "exception": true, "timeout": true, "transient": true}

// unchecked answers which coverage area a finding leaves unevaluated, or "".
func unchecked(f Finding) string {
	if area, ok := uncheckedMessages[f.MessageID]; ok {
		return area
	}
	if uncheckedCodes[f.Code] {
		return "structure"
	}
	return ""
}

// blockedStates names the result state for the first unevaluated area.
var blockedStates = map[string]string{"terminology": "terminology-unavailable", "invariants": "invariant-unsupported", "profiles": "profile-unavailable", "structure": "check-unavailable"}
