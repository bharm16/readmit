// The shared display vocabulary: the mandatory captions, field states kept
// distinct, and an unknown code shown as Unsupported with its exact code rather
// than a guessed meaning.
import { expect, test } from "vitest";
import { render, screen } from "@testing-library/react";
import {
  DisplayTerm,
  FIELD_STATES,
  HIDDEN_VALUE,
  RESET_OPERATORS,
  TARGET_CLASSIFICATIONS,
  TEST_BOUNDARIES,
  TEST_RESULTS,
  term,
} from "./display";

test("the mandatory codes read as their captions", () => {
  expect(term(TEST_RESULTS, "assertion_failure").text).toBe("Failed");
  expect(term(TEST_RESULTS, "execution_error").text).toBe("Error");
  expect(term(TARGET_CLASSIFICATIONS, "unclassified").text).toBe("Not classified");
  expect(term(RESET_OPERATORS, "operator_confirms").text).toBe("Manual confirmation");
  expect(term(TEST_BOUNDARIES, "appointment-ledger").text).toBe("Appointment records");
  expect(term(TEST_BOUNDARIES, "ack-contract").text).toBe("Acknowledgements");
});

test("present, empty, null, not present and hidden stay five different words", () => {
  const words = [...Object.values(FIELD_STATES), HIDDEN_VALUE];
  expect(words).toEqual(["Present", "Empty", "Null", "Not present", "Hidden"]);
  expect(new Set(words).size).toBe(5);
});

test("an unknown member is Unsupported and keeps its exact code, never a guess", () => {
  expect(term(TEST_RESULTS, "flaky_pass")).toEqual({ text: "Unsupported", supported: false, code: "flaky_pass" });
  // Inherited object keys are not captions.
  expect(term(TEST_RESULTS, "toString").supported).toBe(false);
  render(<DisplayTerm map={TEST_RESULTS} code="flaky_pass" />);
  expect(screen.getByText("Unsupported")).toBeTruthy();
  expect(screen.getByText("flaky_pass").tagName).toBe("CODE");
});
