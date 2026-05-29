FROM golang:1.21-alpine

RUN addgroup -S sandbox && adduser -S -G sandbox sandbox && \
    mkdir -p /sandbox /goexec-cache && \
    chown sandbox:sandbox /sandbox /goexec-cache

USER sandbox
ENV GOCACHE=/goexec-cache

# Pre-warm the Go build cache as the sandbox user.
# This compiles the stdlib and bakes the artifacts into the image layer,
# so the first real submission runs in <1s instead of 5-8s.
RUN cd /tmp && \
    printf 'package main\nimport "fmt"\nfunc main(){fmt.Println("ok")}' > warmup.go && \
    go run warmup.go && \
    rm warmup.go

WORKDIR /sandbox
CMD ["sh"]
