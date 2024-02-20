FROM docker.io/library/golang:1.20.4 AS builder

RUN apt-get update && apt-get install -y ca-certificates \
    make \
    git \
    curl \
    mercurial

ARG PACKAGE=github.com/postfinance/kubenurse

RUN mkdir -p /go/src/${PACKAGE}
WORKDIR /go/src/${PACKAGE}

COPY . .
# Build
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -mod=vendor -a -o kubenurse -ldflags "-X main.GitCommit=$(git rev-list -1 HEAD)" ${PACKAGE}



# Copy the binary into a thin image
#FROM alpine:3.9.4
FROM busybox:latest
# RUN apk add --update ca-certificates \
#  && apk add --update -t deps curl jq iproute2 bash \
#  && apk del --purge deps \
#  && rm /var/cache/apk/*

WORKDIR /
COPY --from=builder /go/src/github.com/postfinance/kubenurse/kubenurse /usr/bin/kubenurse
ENTRYPOINT ["/usr/bin/kubenurse"]