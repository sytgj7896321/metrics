FROM golang:1.17 as builder

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
    GOPROXY='https://proxy.golang.com.cn,direct'

WORKDIR /build

RUN git init && \
    git remote add -f origin https://github.com/sytgj7896321/metrics.git && \
    git config core.sparse-checkout true && \
    echo serviceQuotasExporter >> .git/info/sparse-checkout && \
    git reset --hard origin/master

RUN cd serviceQuotasExporter && go build .

FROM scratch

COPY --from=builder /build/serviceQuotasExporter /

ENTRYPOINT ["/serviceQuotasExporter"]