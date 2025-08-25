FROM alpine AS build

RUN apk add --no-cache go 
RUN mkdir -p /app
WORKDIR /app
COPY ./ /app/
RUN go build .


FROM alpine

RUN mkdir -p /app
WORKDIR /app
COPY --from=build /app/tun /app/tun

CMD mkdir -p /dev/net/ && mknod /dev/net/tun c 10 200 && ./tun ${CONFIG}
