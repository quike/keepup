FROM gcr.io/distroless/static-debian12:nonroot@sha256:d093aa3e30dbadd3efe1310db061a14da60299baff8450a17fe0ccc514a16639

ARG TARGETOS
ARG TARGETARCH

COPY --chmod=555 target/builds/${TARGETOS}/${TARGETARCH}/keepup /usr/bin/keepup

USER nonroot:nonroot
ENTRYPOINT ["/usr/bin/keepup"]
