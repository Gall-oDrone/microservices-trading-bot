FROM python:3.11-slim

WORKDIR /app
RUN apt-get update && apt-get install -y --no-install-recommends git ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY services/research-agent/requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY services/research-agent/app ./app

ENV HOST=0.0.0.0
ENV PORT=8091
ENV RESEARCH_READ_ONLY=true
ENV BROKER_TARGET=etoro

EXPOSE 8091
CMD ["uvicorn", "app.main:app", "--host", "0.0.0.0", "--port", "8091"]
