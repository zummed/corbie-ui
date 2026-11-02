# Metrics

The rules behind [the Metrics overview](../metrics.md). Area code: `metrics`.

## The series

**R-metrics-01** Each run is counted in `corbie_runs` by status, failure, type and machine; each running run's jobs on runners, timers and tickets in `corbie_waiting` by type and machine; the oldest running run of each machine in `corbie_oldest_running_seconds`; and when a run of each machine last ended, by status, in `corbie_last_ended_timestamp_seconds`. *Test:* `metrics_series_and_their_labels`

**C-metrics-01** Every series is a gauge. An ended run leaves the store after its keep, so a count of ended runs falls, which a counter may not. *Test:* `metrics_every_series_is_a_gauge`

**C-metrics-02** A scrape fails while the store cannot be read. Answered from the last list, it would show Grafana the last counts as if all were well. *Test:* `metrics_scrape_fails_while_the_store_cannot_be_read`

## Interfaces

Metrics uses no other area of the UI: Serving gives it the list of runs.
