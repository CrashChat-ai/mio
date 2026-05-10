// Package metrics defines Prometheus collectors for the attachment-downloader.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	DownloadedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mio_attachment_downloaded_total",
		Help: "Total attachments processed by channel_type and outcome.",
	}, []string{"channel_type", "outcome"})

	BytesTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mio_attachment_bytes_total",
		Help: "Total bytes persisted to storage by channel_type.",
	}, []string{"channel_type"})

	DedupHits = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mio_attachment_dedup_hits_total",
		Help: "Total dedup hits (HEAD-found or IfNotExists collision) by channel_type.",
	}, []string{"channel_type"})

	DownloadDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "mio_attachment_download_duration_seconds",
		Help:    "Per-attachment fetch+persist wall-clock duration.",
		Buckets: []float64{0.1, 0.5, 1, 2, 5, 10, 30, 60},
	}, []string{"channel_type", "outcome"})

	StorageDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "mio_attachment_storage_duration_seconds",
		Help:    "Backend operation duration (Put/Get/SignedURL).",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5},
	}, []string{"backend", "op"})

	Inflight = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "mio_attachment_inflight",
		Help: "Number of attachments currently being processed.",
	})
)
