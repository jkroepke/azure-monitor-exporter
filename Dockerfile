FROM gcr.io/distroless/static-debian12:nonroot
ENTRYPOINT ["/azure-metrics-exporter"]
COPY azure-metrics-exporter /