FROM gcr.io/distroless/static-debian12:nonroot@sha256:f5b485ea962d9bd1186b2f6b3a061191539b905b82ec395de78cbfae51f20e35

ARG TARGETOS
ARG TARGETARCH

COPY --chmod=555 target/builds/${TARGETOS}/${TARGETARCH}/keepup /usr/bin/keepup

USER nonroot:nonroot
ENTRYPOINT ["/usr/bin/keepup"]
