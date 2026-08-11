# Aetheria service image — one Dockerfile, four binaries via build arg.
# Builds from the repo root so it can see server/ + shared/ + go.mod.
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG SERVICE=authserver
RUN go build -trimpath -o /out/service ./server/cmd/${SERVICE}

FROM alpine:3.20
RUN adduser -D -u 1000 aetheria && apk add --no-cache ca-certificates tzdata
COPY --from=build /out/service /usr/local/bin/service
USER aetheria
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/service"]
