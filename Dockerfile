FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/trader .

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/trader /app/trader
COPY dashboard.html /app/dashboard.html
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["/app/trader", "--dashboard"]
