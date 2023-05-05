name := "my-scala-project"

version := "1.0"

scalaVersion := "2.13.8"

libraryDependencies ++= Seq(
  // "org.apache.spark" %% "spark-core" % "3.2.0",
  // "org.apache.spark" %% "spark-sql" % "3.2.0",
  // "org.apache.spark" %% "spark-streaming" % "3.2.0",
  // "org.apache.spark" %% "spark-streaming-kafka-0-10" % "3.2.0",
  // "com.datastax.spark" %% "spark-cassandra-connector" % "3.2.0",
  "org.scalatest" %% "scalatest" % "3.2.9" % Test
)

// assemblyMergeStrategy in assembly := {
//   case PathList("META-INF", xs@_*) => MergeStrategy.discard
//   case x => MergeStrategy.first
// }
