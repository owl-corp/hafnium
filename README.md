# Hafnium

Hafnium is a standalone Go daemon that synchronizes Keycloak role memberships with GitHub organization and team memberships.

## Features

- **Concurrent Sync:** Optimized data fetching from Keycloak and GitHub using Go's concurrency primitives.
- **Organization Sync:** Automatically adds/removes members from the GitHub organization based on Keycloak linked identities.
- **Team Sync:** Synchronizes Keycloak roles to GitHub team memberships using a configurable mapping.
- **Invitation Management:**
  - Sends Discord DMs for new organization invitations.
  - Automatically cleans up and notifies users about failed/expired invitations.
- **Observability:**
  - Prometheus metrics (`/metrics`) for monitoring sync performance and state.
  - Structured logging for easy troubleshooting.
- **Configuration:** Flexible configuration via environment variables and TOML mapping files.

## Configuration

Hafnium is configured via environment variables (prefixed with `HAFNIUM_`), CLI flags, or a configuration file.

### CLI Help

For a full list of configuration options and flags, run:

```bash
./hafnium --help
```

### Role Mapping (`mappings.toml`)

Create a TOML file to map Keycloak roles to GitHub team slugs and Discord roles:

```toml
[mappings]
helpers = { github_team_slug = "helpers", discord_role_id = 267630620367257601 }
devops = { github_team_slug = "devops", discord_role_id = 409416496733880320 }
```

## Metrics

Hafnium exposes the following Prometheus metrics:

- `hafnium_org_members_total`: Total organization members.
- `hafnium_team_members_total{team="slug"}`: Total members in a specific team.
- `hafnium_keycloak_users_total`: Total users found in Keycloak.
- `hafnium_org_added_total`: Counter for users added to the org.
- `hafnium_org_removed_total`: Counter for users removed from the org.
- `hafnium_team_added_total{team="slug"}`: Counter for users added to a team.
- `hafnium_team_removed_total{team="slug"}`: Counter for users removed from a team.
- `hafnium_sync_duration_seconds`: Histogram of sync task duration.

## Development

### Building Locally

```bash
go build -o hafnium ./cmd/hafnium
```

### Running with Docker

```bash
docker build -t hafnium .
docker run --env-file .env hafnium
```

### CLI Help

You can view usage information and configuration details by running:

```bash
./hafnium --help
```

## License

[MIT](LICENSE)
