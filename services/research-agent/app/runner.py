"""Runs Gall-oDrone/TradingAgents graph for cold-path research (no order placement)."""

from __future__ import annotations

import logging
import uuid
from datetime import datetime, timezone

from tradingagents.default_config import DEFAULT_CONFIG
from tradingagents.graph.trading_graph import TradingAgentsGraph

logger = logging.getLogger(__name__)


def run_trading_agents(ticker: str, trade_date: str, news_context: dict | None) -> dict:
    config = DEFAULT_CONFIG.copy()
    config["research_depth"] = config.get("research_depth", 1)
    graph = TradingAgentsGraph(debug=False, config=config)
    _, decision = graph.propagate(ticker.upper(), trade_date)
    return {
        "run_id": str(uuid.uuid4()),
        "timestamp_ms": int(datetime.now(timezone.utc).timestamp() * 1000),
        "ticker": ticker.upper(),
        "trade_date": trade_date,
        "decision": str(decision),
        "framework": "trading-agents",
        "broker_target": "none",
        "news_context_used": news_context is not None,
        "news_context": news_context,
        "read_only": True,
        "warning": "Cold-path memo only. Does not place orders on eToro or Bitso.",
    }
