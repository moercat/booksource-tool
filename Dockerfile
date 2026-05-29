FROM alpine:3.22

WORKDIR /app
COPY booksource-tool /usr/local/bin/booksource-tool
ENTRYPOINT ["/usr/local/bin/booksource-tool"]
