import os

HOST = os.getenv("HOST", "0.0.0.0")
PORT = int(os.getenv("PORT", "8091"))
NEWS_PUBLISHER_URL = os.getenv("NEWS_PUBLISHER_URL", "http://news-publisher:8090")
BROKER_TARGET = os.getenv("BROKER_TARGET", "etoro")
READ_ONLY = os.getenv("RESEARCH_READ_ONLY", "true").lower() in ("1", "true", "yes")
KAFKA_BROKERS = os.getenv("KAFKA_BROKERS", "")
KAFKA_TOPIC_RESEARCH_MEMOS = os.getenv("KAFKA_TOPIC_RESEARCH_MEMOS", "research.memos")
KAFKA_PUBLISH_MEMOS = os.getenv("KAFKA_PUBLISH_MEMOS", "false").lower() in ("1", "true", "yes")
RESEARCH_S3_BUCKET = os.getenv("RESEARCH_S3_BUCKET", "")
RESEARCH_S3_PREFIX = os.getenv("RESEARCH_S3_PREFIX", "research/memos/")
RESEARCH_S3_ENABLED = os.getenv("RESEARCH_S3_ENABLED", "true").lower() in ("1", "true", "yes")
