package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	OrgMembersTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "hafnium_org_members_total",
		Help: "Total number of members in the GitHub organization.",
	})
	TeamMembersTotal = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "hafnium_team_members_total",
		Help: "Total number of members in a GitHub team.",
	}, []string{"team"})
	KeycloakUsersTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "hafnium_keycloak_users_total",
		Help: "Total number of users found in Keycloak.",
	})
	OrgAddedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "hafnium_org_added_total",
		Help: "Total number of users added to the GitHub organization.",
	})
	OrgRemovedTotal = promauto.NewCounter(prometheus.CounterOpts{
		Name: "hafnium_org_removed_total",
		Help: "Total number of users removed from the GitHub organization.",
	})
	TeamAddedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hafnium_team_added_total",
		Help: "Total number of users added to a GitHub team.",
	}, []string{"team"})
	TeamRemovedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "hafnium_team_removed_total",
		Help: "Total number of users removed from a GitHub team.",
	}, []string{"team"})
	SyncDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "hafnium_sync_duration_seconds",
		Help:    "Duration of the sync task in seconds.",
		Buckets: prometheus.DefBuckets,
	})
)
