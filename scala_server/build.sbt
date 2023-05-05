name := "scala_server"

version := "0.1"

scalaVersion := "2.13.6"

val sparkVersion = "3.2.0"
val kafkaVersion = "2.8.1"
val cassandraVersion = "4.0.1"

libraryDependencies ++= Seq(
  "org.apache.spark" %% "spark-core" % sparkVersion,
  "org.apache.spark" %% "spark-sql" % sparkVersion,
  "org.apache.spark" %% "spark-streaming" % sparkVersion,
  "org.apache.spark" %% "spark-streaming-kafka-0-10" % sparkVersion,
  "com.datastax.oss" % "java-driver-core" % cassandraVersion,
  "com.datastax.oss" % "java-driver-query-builder" % cassandraVersion,
  "com.datastax.spark" %% "spark-cassandra-connector" % "3.1.0",
  "org.apache.kafka" %% "kafka" % kafkaVersion
)

addSbtPlugin("com.eed3si9n" % "sbt-assembly" % "1.2.0")
