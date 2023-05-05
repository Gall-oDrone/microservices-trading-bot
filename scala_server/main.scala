import org.apache.spark._
import org.apache.spark.streaming._
import org.apache.spark.streaming.kafka._

object KafkaSparkStreaming {
  def main(args: Array[String]) {
    val sparkConf = new SparkConf().setAppName("KafkaSparkStreaming")
    val ssc = new StreamingContext(sparkConf, Seconds(1))

    val kafkaParams = Map("bootstrap.servers" -> "localhost:9092")
    val topics = Set("test")

    val kafkaStream = KafkaUtils.createDirectStream[String, String, StringDecoder, StringDecoder](
      ssc,
      kafkaParams,
      topics
    )

    kafkaStream.map(_._2).flatMap(_.split(" ")).map(word => (word, 1)).reduceByKey(_ + _).print()

    ssc.start()
    ssc.awaitTermination()
  }
}
