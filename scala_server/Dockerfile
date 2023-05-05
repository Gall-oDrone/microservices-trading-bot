
FROM adoptopenjdk/openjdk11:latest AS builder

RUN apt-get update && \
    apt-get install -y apt-transport-https curl && \
    curl -L -o sbt.tgz https://github.com/sbt/sbt/releases/download/v1.5.5/sbt-1.5.5.tgz && \
    tar -xzf sbt.tgz && \
    rm sbt.tgz

WORKDIR /sbt/bin

RUN ./sbt exit

WORKDIR /app

COPY . .

RUN ls

RUN /sbt/bin/sbt assembly

FROM adoptopenjdk/openjdk11:latest

WORKDIR /app

COPY --from=builder /app/target/scala-2.13/kafka-spark-cassandra.jar /app/kafka-spark-cassandra.jar

CMD ["java", "-jar", "kafka-spark-cassandra.jar"]
