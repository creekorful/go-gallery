FROM scratch

ADD go-gallery /usr/bin/go-gallery

ENTRYPOINT ["/usr/bin/go-gallery"]