# Buttons CLI runtime image.
#
# This image is built by goreleaser from prebuilt binaries — it does NOT
# compile Go inside the container. The binary is copied in from the
# dist/ tree goreleaser populates per-arch. Build goreleaser-wise via:
#
#   goreleaser release --snapshot --clean
#
# Or manually with a binary in the current directory:
#
#   go build -o buttons .
#   docker build -t buttons:dev .
#
# Final image: ~12 MB. Runs as non-root UID 1000 by default.

FROM alpine:3.20

# ca-certificates: HTTPS outbound for HTTP buttons
# tzdata:          timestamp formatting in history + run records
RUN apk add --no-cache \
        ca-certificates \
        tzdata \
    && adduser -D -u 1000 buttons

# Binary is injected at build time by goreleaser's dockers: block.
# For manual builds, place a `buttons` binary next to this Dockerfile.
COPY buttons /usr/local/bin/buttons

USER buttons
WORKDIR /home/buttons

# ENTRYPOINT as the binary so `docker run buttons press foo` works.
# CMD defaults to --help so `docker run buttons` (no args) prints usage
# instead of silently exiting.
ENTRYPOINT ["buttons"]
CMD ["--help"]
