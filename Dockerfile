FROM scratch

COPY --chmod=555 target/builds/linux/keepup /usr/bin/keepup

ENTRYPOINT ["/usr/bin/keepup"]
