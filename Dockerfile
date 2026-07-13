FROM gcr.io/distroless/static-debian12:nonroot@sha256:b7bb25d9f7c31d2bdd1982feb4dafcaf137703c7075dbe2febb41c24212b946f

ARG TARGETOS
ARG TARGETARCH

COPY --chmod=555 target/builds/${TARGETOS}/${TARGETARCH}/keepup /usr/bin/keepup

USER nonroot:nonroot
ENTRYPOINT ["/usr/bin/keepup"]
