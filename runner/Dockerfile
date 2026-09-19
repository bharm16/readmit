# Stage the customer-verified static Linux readmit binary in this build context.
FROM scratch
COPY readmit /readmit
USER 65532:65532
ENTRYPOINT ["/readmit", "runner", "serve", "/data/inbox", "--config", "/config/runner.json", "--send"]
