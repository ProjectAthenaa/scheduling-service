#build stage
FROM golang:1.16.7-alpine3.14 AS build-env

ARG GH_TOKEN
RUN apk add build-base git
RUN git config --global url."https://${GH_TOKEN}:x-oauth-basic@github.com/ProjectAthenaa".insteadOf "https://github.com/ProjectAthenaa"
RUN mkdir /app
ADD . /app
WORKDIR /app
RUN --mount=type=cache,target=/root/.cache/go-build
RUN go build -ldflags "-s -w" -o goapp


# final stage
FROM alpine
WORKDIR /app
COPY --from=build-env /app/goapp /app/

ENV ELASTIC_APM_SERVER="https://4f8d53c3f63f4c138ff4367b7ebc3967.apm.us-east-1.aws.cloud.es.io:443"
ENV ELASTIC_APM_SERVICE_NAME="Scheduling Service"
ENV ELASTIC_APM_SECRET_TOKEN="aBl5cy0EpPbRDLEC6U"
ENV ELASTIC_APM_ENVIRONMENT="DEV"
ENV REDIS_URL="rediss://default:kulqkv6en3um9u09@athena-redis-do-user-9223163-0.b.db.ondigitalocean.com:25061"
ENV PG_URL="postgresql://doadmin:rh3rc0vgg1f706kz@athenadb-do-user-9223163-0.b.db.ondigitalocean.com:25060/defaultdb"

EXPOSE 8080 8080

ENTRYPOINT ./goapp