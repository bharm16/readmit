// The facade in a journey does real work over real files, so a screen waits
// for what the application actually answers rather than the component tests'
// immediate stubs. The bound is generous and finite: a journey that never
// draws what it waits for still fails.
import { configure } from "@testing-library/react";

configure({ asyncUtilTimeout: 30_000 });
