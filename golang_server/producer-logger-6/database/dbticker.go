func (rc *RedisClient) PostTicker(ticker bitso.Ticker) error {
	// Convert the Ticker struct to JSON
	tickerJSON, err := json.Marshal(ticker)
	if err != nil {
		return err
	}

	// Use SET command to store the JSON data in Redis
	err = rc.client.Set(rc.ctxbg, "ticker", tickerJSON, 0).Err()
	if err != nil {
		return err
	}

	return nil
}

func (rc *RedisClient) GetTicker() (bitso.Ticker, error) {
	var ticker bitso.Ticker
	// Retrieve the JSON data from Redis
	tickerJSON, err := rc.client.Get(rc.ctxbg, "ticker_data").Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			// Key does not exist in Redis
			return ticker, errors.New("ticker data not found in Redis")
		}
		// Other error occurred
		return ticker, err
	}

	// Unmarshal the JSON data into a Ticker struct
	err = json.Unmarshal([]byte(tickerJSON), &ticker)
	if err != nil {
		return ticker, err
	}

	return ticker, nil
}