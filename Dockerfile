FROM gcr.io/distroless/static-debian12:nonroot

ARG TARGETOS
ARG TARGETARCH

COPY --chmod=555 target/builds/${TARGETOS}/${TARGETARCH}/keepup /usr/bin/keepup

USER nonroot:nonroot
ENTRYPOINT ["/usr/bin/keepup"]
