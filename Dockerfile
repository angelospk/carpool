# Build the API as a static binary, then ship it on a distroless base.
#
# The result is a single binary plus CA certificates and tzdata. tzdata matters
# more than it looks: all scheduling is done in Europe/Athens, and a container
# without the zone database would silently fall back to UTC and materialize
# every recurring trip an hour or two off.

FROM golang:1.25-bookworm AS build

WORKDIR /src

# Dependencies first, so a code change does not re-download the module cache.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO off keeps the binary static; trimpath and -s -w drop local paths and debug
# tables from the image.
RUN CGO_ENABLED=0 GOOS=linux go build \
      -trimpath -ldflags="-s -w" \
      -o /out/api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/api /api
# Region fixtures are seeded at boot when REGION_FIXTURE points at one.
COPY --from=build /src/fixtures /fixtures

USER nonroot:nonroot
EXPOSE 8080

ENTRYPOINT ["/api"]
