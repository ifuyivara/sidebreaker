# Introduction
This is a very simple sidecar circuit breaker written in Go. It is intended to be deployed next to an application that makes HTTP calls to external services and act as a proxy monitoring the error rates of each service and tripping a circuit breaker when needed.

Circuit breaker is a pattern in which you monitor failures in calls to a service. Once the failures reach a certain threshold the circuit breaker trips and any future calls will return immediately with an error. This is to avoid flooding an unresponsive service and to also avoid slowing your service down while it waits for the unresponsive calls to come back. [You can read more about this pattern here](http://microservices.io/patterns/reliability/circuit-breaker.html).

Sidecar is a pattern where you deploy a smaller application next to your main application allowing it to be composed of heterogeneous components and technologies. When using docker they are usually deployed inside the same container as the main application and the two communicate back and forth inside the same container. [You can read more about this pattern here](https://docs.microsoft.com/en-us/azure/architecture/patterns/sidecar).

## Current state
This project is in active development and has not been tested in production yet. PRs are welcome. 

The current supported features are:

* HTTP proxy of calls via CONNECT only (https and http).
* Supports three different types of circuit breakers. Consecutive Errors (default), Simple Error Threshold and Error rate.
* Configuration of hosts, breaker type and thresholds via config JSON file.
* The circuit breaker error increases on timeouts and connection errors only. Any response from the external service will count as a success, even if it’s an http error response.
* When a circuit breaker is tripped the sidebreaker will only allow a small number of calls to go through in order to test if the external service is back to normal, the circuit breaker closes once a successful response is received.

## Configuration and use

You can create a configuration file in the same folder as the sidebreaker and indicate how you want it to handle the different hosts. Any call to hosts not indicated in the configuration will be proxied normally without any monitoring on the errors.

```javascript
{
	"port": 3129,
	"verbose": true,
	"hosts": [
		{
			"host": "google.com",
			"breakType": "consecutive",
			"timeout": 1000,
			"threshold": 10
		},
		{
			"host": "slowservice.com",
			"breakType": "threshold",
			"timeout": 5000,
			"threshold": 50
		},
		{
			"host": "external.service.com",
			"breakType": "rate",
			"timeout": 8000,
			"threshold": 0.85
		}
	]
}
```

You can indicate 3 types of circuit breaker: consecutive, threshold and rate. The threshold for the rate circuit breaker is a double indicating the percentage per 100 requests before the circuit breaker trips. (i.e. 0.85 if you want 85%).
Once you have your configuration file in the same folder as your sidebreaker you can just start the application normally

`> sidebreaker.exe` or `$ sidebreaker` in linux

The application will log to stdout.