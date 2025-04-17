FROM scratch

ARG CLIOS
ARG CLIOSARCH

COPY --chmod=555 target/builds/linux/keepup /usr/bin/keepup
RUN chmod +x /usr/bin/keepup

ENTRYPOINT ["/usr/bin/keepup"]
