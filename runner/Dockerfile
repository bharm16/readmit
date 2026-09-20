# Stage the customer-verified static Linux readmit binary in this build context.
FROM scratch
COPY readmit /readmit
USER 65532:65532
ENTRYPOINT ["/readmit", "--operation-policy", "/config/operation-policy.json", "runner", "serve", "/data/inbox", "--config", "/config/runner.json", "--send"]
