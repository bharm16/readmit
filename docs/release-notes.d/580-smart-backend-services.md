Added engine support for explicitly authorized SMART Backend Services 2.2 clients
with RS384/ES384 signing, scoped protected HTTP requests, reviewed discovery,
in-memory renewal and redacted failures. Credentials stay in the configured
customer secret provider. Customer registration and permissions are required;
this does not add desktop fields or claim EHR compatibility.
