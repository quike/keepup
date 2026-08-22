FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

ARG TARGETOS
ARG TARGETARCH

COPY --chmod=555 target/builds/${TARGETOS}/${TARGETARCH}/keepup /usr/bin/keepup

USER nonroot:nonroot
ENTRYPOINT ["/usr/bin/keepup"]
