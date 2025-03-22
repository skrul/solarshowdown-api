# Solar Showdown API

A simple HTTP server that exposes solar metrics from InfluxDB as JSON endpoints.

## Configuration

The following environment variables are required:

```
INFLUXDB_URL=http://your-influxdb-host:8086
INFLUXDB_TOKEN=your-influxdb-token
INFLUXDB_ORG=your-organization
INFLUXDB_BUCKET=your-bucket
DONGLE=your-dongle-identifier
SERVER_PORT=8080  # Optional, defaults to 8080
```

The `DONGLE` parameter is used to filter metrics for a specific dongle identifier in the InfluxDB queries.

## Building and Running

```bash
# Build the server
go build -o solarshowdown-api

# Run the server
./solarshowdown-api
```

## API Endpoints

### GET /solarshowdown

Retrieves solar metrics for a specified timeframe.

Query Parameters:
- `timeframe`: (optional) The time range for the metrics. Values: "day" (default), "week", "month"

Example Request:
```bash
curl "http://localhost:8080/solarshowdown?timeframe=week"
```

Example Response:
```json
{
    "timeframe": "week",
    "data": [
        {
            "time": "2024-03-15T12:00:00Z",
            "value": 42.5,
            "field": "power_output"
        }
    ]
}
```

## Error Handling

The API returns appropriate HTTP status codes and error messages in the response body when something goes wrong:

- 400 Bad Request: Invalid timeframe parameter
- 500 Internal Server Error: InfluxDB connection or query errors