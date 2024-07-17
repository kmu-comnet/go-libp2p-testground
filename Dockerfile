FROM go:latest as builder
WORKDIR /usr/src/testplan

RUN mkdir ./node
COPY ./node ./node

RUN mkdir ./content
COPY ./content ./content

EXPOSE 3000
ENTRYPOINT ["./node"]