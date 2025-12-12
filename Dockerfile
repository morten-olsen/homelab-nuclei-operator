# Build the manager binary
FROM golang:1.24 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the Go source (relies on .dockerignore to filter)
COPY . .

# Build
# the GOARCH has no default value to allow the binary to be built according to the host where the command
# was called. For example, if we call make docker-build in a local env which has the Apple Silicon M1 SO
# the docker BUILDPLATFORM arg will be linux/arm64 when for Apple x86 it will be linux/amd64. Therefore,
# by leaving it empty we can ensure that the container and binary shipped on it will have the same platform.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -o manager cmd/main.go

# Final image
FROM alpine:3.19 AS final

# Build arguments for nuclei version and architecture
ARG TARGETOS
ARG TARGETARCH
ARG NUCLEI_VERSION=3.6.0

# Install ca-certificates for HTTPS requests, curl for downloading, and create non-root user
RUN apk --no-cache add ca-certificates tzdata curl unzip && \
  adduser -D -u 65532 -g 65532 nonroot

# Download prebuilt nuclei binary
# ProjectDiscovery uses different naming: linux_amd64, linux_arm64
RUN NUCLEI_ARCH=$(echo ${TARGETARCH} | sed 's/amd64/amd64/;s/arm64/arm64/') && \
  curl -sSL "https://github.com/projectdiscovery/nuclei/releases/download/v${NUCLEI_VERSION}/nuclei_${NUCLEI_VERSION}_linux_${NUCLEI_ARCH}.zip" -o /tmp/nuclei.zip && \
  unzip /tmp/nuclei.zip -d /tmp && \
  mv /tmp/nuclei /usr/local/bin/nuclei && \
  chmod +x /usr/local/bin/nuclei && \
  rm -rf /tmp/nuclei.zip /tmp/nuclei && \
  apk del curl unzip

# Create directories for nuclei
RUN mkdir -p /nuclei-templates /home/nonroot/.nuclei && \
  chown -R 65532:65532 /nuclei-templates /home/nonroot

WORKDIR /

# Copy the manager binary
COPY --from=builder /workspace/manager .

# Set ownership
RUN chown 65532:65532 /manager /usr/local/bin/nuclei

# Use non-root user
USER 65532:65532

# Environment variables for nuclei
ENV NUCLEI_TEMPLATES_PATH=/nuclei-templates
ENV HOME=/home/nonroot

ENTRYPOINT ["/manager"]
