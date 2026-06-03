package sync

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/owl-corp/hafnium/config"
	"github.com/owl-corp/hafnium/pkg/discord"
	"github.com/owl-corp/hafnium/pkg/github"
	"github.com/owl-corp/hafnium/pkg/keycloak"
	"github.com/owl-corp/hafnium/pkg/metrics"
)

const (
	GithubInviteLink    = "https://github.com/orgs/%s/invitation"
	KeycloakAccountURL = "%s/realms/%s/account/account-security/linked-accounts"
)

type Engine struct {
	config   *config.Config
	keycloak *keycloak.Client
	github   *github.Client
	discord  *discord.Client

	resolvedLoginsCache map[string]string
}

func NewEngine(cfg *config.Config, k *keycloak.Client, g *github.Client, d *discord.Client) *Engine {
	return &Engine{
		config:              cfg,
		keycloak:            k,
		github:              g,
		discord:             d,
		resolvedLoginsCache: make(map[string]string),
	}
}

func (e *Engine) getGithubInviteLink() string {
	return fmt.Sprintf(GithubInviteLink, e.config.Github.Org)
}

func (e *Engine) getKeycloakAccountLink() string {
	baseURL := strings.TrimSuffix(e.config.Keycloak.URL, "/")
	return fmt.Sprintf(KeycloakAccountURL, baseURL, e.config.Keycloak.Realm)
}

type CommonInfo struct {
	KeycloakIdentities       map[string]keycloak.Identity // KeycloakUsername -> Identity
	KeycloakUserRoles        map[string][]string          // KeycloakUserID -> RoleNames
	KeycloakDiscordIDs       map[string]string            // KeycloakUserID -> DiscordID
	KeycloakUserIDByUsername map[string]string            // KeycloakUsername -> KeycloakUserID
	KeycloakUserEnabled      map[string]bool              // KeycloakUserID -> bool
	GitHubOrgMembersByID     map[string]string            // GitHubID -> Login
	ResolvedLoginsByUserID   map[string]string            // GitHubID -> Login
	PendingInvitations       map[string]struct{}
	FailedInvitations        map[string]struct{}
}

func (e *Engine) FetchCommonInfo(ctx context.Context) (*CommonInfo, error) {
	log.Println("Fetching common info...")

	kcUsers, err := e.keycloak.GetUsers(ctx)
	if err != nil {
		return nil, err
	}

	kcIdentities := make(map[string]keycloak.Identity)
	kcUserRoles := make(map[string][]string)
	kcDiscordIDs := make(map[string]string)
	kcUserIDByUsername := make(map[string]string)
	kcUserEnabled := make(map[string]bool)

	type userResult struct {
		username  string
		userID    string
		ident     *keycloak.Identity
		roles     []string
		discordID string
		enabled   bool
		err       error
	}

	resultChan := make(chan userResult, len(kcUsers))
	sem := make(chan struct{}, e.config.Sync.Parallelism)

	for _, u := range kcUsers {
		var attributes map[string][]string
		if u.Attributes != nil {
			attributes = *u.Attributes
		}
		enabled := true
		if u.Enabled != nil {
			enabled = *u.Enabled
		}
		go func(username, userID string, attrs map[string][]string, enabled bool) {
			sem <- struct{}{}
			defer func() { <-sem }()

			res := userResult{
				username: username,
				userID:   userID,
				enabled:  enabled,
			}

			ident, err := e.keycloak.GetUserFederatedIdentity(ctx, userID, e.config.Keycloak.Provider)
			if err != nil {
				res.err = fmt.Errorf("failed to get %s identity for user %s: %w", e.config.Keycloak.Provider, username, err)
				resultChan <- res
				return
			}
			res.ident = ident

			roles, err := e.keycloak.GetUserRoles(ctx, userID)
			if err != nil {
				res.err = fmt.Errorf("failed to get roles for user %s: %w", username, err)
				resultChan <- res
				return
			}
			for _, r := range roles {
				res.roles = append(res.roles, *r.Name)
			}

			if discordIDs, ok := attrs["discordId"]; ok && len(discordIDs) > 0 {
				res.discordID = discordIDs[0]
			}

			resultChan <- res
		}(*u.Username, *u.ID, attributes, enabled)
	}

	for i := 0; i < len(kcUsers); i++ {
		res := <-resultChan
		if res.err != nil {
			return nil, res.err
		}
		kcUserIDByUsername[res.username] = res.userID
		if res.ident != nil {
			kcIdentities[res.username] = *res.ident
		}
		kcUserRoles[res.userID] = res.roles
		kcUserEnabled[res.userID] = res.enabled
		if res.discordID != "" {
			kcDiscordIDs[res.userID] = res.discordID
		}
	}

	githubOrgMembers, err := e.github.ListOrgMembers(ctx)
	if err != nil {
		return nil, err
	}

	for id, login := range githubOrgMembers {
		e.resolvedLoginsCache[id] = login
	}

	resolvedLoginsByUserID := make(map[string]string)
	for id, login := range githubOrgMembers {
		resolvedLoginsByUserID[id] = login
	}

	type githubResult struct {
		userID string
		login  string
		err    error
	}

	var unresolvedIDs []string
	for _, ident := range kcIdentities {
		if _, ok := resolvedLoginsByUserID[ident.UserID]; ok {
			continue
		}
		if cached, ok := e.resolvedLoginsCache[ident.UserID]; ok {
			resolvedLoginsByUserID[ident.UserID] = cached
			continue
		}
		unresolvedIDs = append(unresolvedIDs, ident.UserID)
	}

	if len(unresolvedIDs) > 0 {
		ghResChan := make(chan githubResult, len(unresolvedIDs))
		for _, id := range unresolvedIDs {
			go func(userID string) {
				sem <- struct{}{}
				defer func() { <-sem }()

				idInt, _ := strconv.ParseInt(userID, 10, 64)
				resolvedLogin, err := e.github.GetUsernameForID(ctx, idInt)
				if err != nil {
					ghResChan <- githubResult{userID: userID, err: fmt.Errorf("could not resolve login for GitHub user ID %s: %w", userID, err)}
					return
				}
				ghResChan <- githubResult{userID: userID, login: resolvedLogin}
			}(id)
		}

		for i := 0; i < len(unresolvedIDs); i++ {
			res := <-ghResChan
			if res.err != nil {
				return nil, res.err
			}
			if res.login != "" {
				resolvedLoginsByUserID[res.userID] = res.login
				e.resolvedLoginsCache[res.userID] = res.login
			}
		}
	}

	pending, err := e.github.ListPendingInvitations(ctx)
	if err != nil {
		return nil, err
	}
	failed, err := e.github.ListFailedInvitations(ctx)
	if err != nil {
		return nil, err
	}

	metrics.KeycloakUsersTotal.Set(float64(len(kcUsers)))
	metrics.OrgMembersTotal.Set(float64(len(githubOrgMembers)))

	return &CommonInfo{
		KeycloakIdentities:       kcIdentities,
		KeycloakUserRoles:        kcUserRoles,
		KeycloakDiscordIDs:       kcDiscordIDs,
		KeycloakUserIDByUsername: kcUserIDByUsername,
		KeycloakUserEnabled:      kcUserEnabled,
		GitHubOrgMembersByID:     githubOrgMembers,
		ResolvedLoginsByUserID:   resolvedLoginsByUserID,
		PendingInvitations:       pending,
		FailedInvitations:        failed,
	}, nil
}

type MembershipDiff struct {
	ToAdd    []string
	ToRemove []string
	ToKeep   []string
}

type OrgSyncPlan struct {
	Diff           MembershipDiff
	SkippedPending []string
	SkippedFailed  []string
}

func (e *Engine) getIgnoredUsers() map[string]struct{} {
	ignored := make(map[string]struct{})
	for _, u := range e.config.Github.IgnoredUsers {
		ignored[strings.ToLower(u)] = struct{}{}
	}
	return ignored
}

func (e *Engine) BuildOrgSyncPlan(info *CommonInfo) *OrgSyncPlan {
	ignored := e.getIgnoredUsers()

	desiredByUserID := make(map[string]string)
	for username, ident := range info.KeycloakIdentities {
		userID := info.KeycloakUserIDByUsername[username]
		if !info.KeycloakUserEnabled[userID] {
			continue
		}
		if login, ok := info.ResolvedLoginsByUserID[ident.UserID]; ok {
			if _, ok := ignored[strings.ToLower(login)]; !ok {
				desiredByUserID[ident.UserID] = login
			}
		}
	}

	githubByUserID := make(map[string]string)
	for id, login := range info.GitHubOrgMembersByID {
		if _, ok := ignored[strings.ToLower(login)]; !ok {
			githubByUserID[id] = login
		}
	}

	toAddUserIDs := make(map[string]struct{})
	for id := range desiredByUserID {
		if _, ok := githubByUserID[id]; !ok {
			toAddUserIDs[id] = struct{}{}
		}
	}

	toRemoveUserIDs := make(map[string]struct{})
	for id := range githubByUserID {
		if _, ok := desiredByUserID[id]; !ok {
			toRemoveUserIDs[id] = struct{}{}
		}
	}

	keptUserIDs := make(map[string]struct{})
	for id := range desiredByUserID {
		if _, ok := githubByUserID[id]; ok {
			keptUserIDs[id] = struct{}{}
		}
	}

	var skippedPending []string
	var skippedFailed []string
	var actionableToAdd []string

	ids := make([]string, 0, len(toAddUserIDs))
	for id := range toAddUserIDs {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	for _, id := range ids {
		login := desiredByUserID[id]
		norm := strings.ToLower(login)
		if _, ok := info.PendingInvitations[norm]; ok {
			skippedPending = append(skippedPending, login)
			continue
		}
		if _, ok := info.FailedInvitations[norm]; ok {
			skippedFailed = append(skippedFailed, login)
			continue
		}
		actionableToAdd = append(actionableToAdd, login)
	}

	var toRemove []string
	removeIDs := make([]string, 0, len(toRemoveUserIDs))
	for id := range toRemoveUserIDs {
		removeIDs = append(removeIDs, id)
	}
	sort.Strings(removeIDs)
	for _, id := range removeIDs {
		toRemove = append(toRemove, githubByUserID[id])
	}

	var toKeep []string
	keepIDs := make([]string, 0, len(keptUserIDs))
	for id := range keptUserIDs {
		keepIDs = append(keepIDs, id)
	}
	sort.Strings(keepIDs)
	for _, id := range keepIDs {
		toKeep = append(toKeep, desiredByUserID[id])
	}

	return &OrgSyncPlan{
		Diff: MembershipDiff{
			ToAdd:    actionableToAdd,
			ToRemove: toRemove,
			ToKeep:   toKeep,
		},
		SkippedPending: skippedPending,
		SkippedFailed:  skippedFailed,
	}
}

type TeamSyncPlan struct {
	TeamSlug string
	Diff     MembershipDiff
}

func (e *Engine) BuildTeamSyncPlan(teamSlug string, desired, current []string, orgUsersToRemoveNorm map[string]struct{}) *TeamSyncPlan {
	ignored := e.getIgnoredUsers()

	desiredByNorm := make(map[string]string)
	for _, u := range desired {
		norm := strings.ToLower(u)
		if _, ok := ignored[norm]; !ok {
			desiredByNorm[norm] = u
		}
	}

	currentByNorm := make(map[string]string)
	for _, u := range current {
		norm := strings.ToLower(u)
		if _, ok := ignored[norm]; ok {
			continue
		}
		if _, ok := orgUsersToRemoveNorm[norm]; ok {
			continue
		}
		currentByNorm[norm] = u
	}

	var toAdd []string
	desiredNorms := make([]string, 0, len(desiredByNorm))
	for norm := range desiredByNorm {
		desiredNorms = append(desiredNorms, norm)
	}
	sort.Strings(desiredNorms)
	for _, norm := range desiredNorms {
		if _, ok := currentByNorm[norm]; !ok {
			toAdd = append(toAdd, desiredByNorm[norm])
		}
	}

	var toRemove []string
	currentNorms := make([]string, 0, len(currentByNorm))
	for norm := range currentByNorm {
		currentNorms = append(currentNorms, norm)
	}
	sort.Strings(currentNorms)
	for _, norm := range currentNorms {
		if _, ok := desiredByNorm[norm]; !ok {
			toRemove = append(toRemove, currentByNorm[norm])
		}
	}

	var toKeep []string
	for _, norm := range desiredNorms {
		if _, ok := currentByNorm[norm]; ok {
			toKeep = append(toKeep, desiredByNorm[norm])
		}
	}

	return &TeamSyncPlan{
		TeamSlug: teamSlug,
		Diff: MembershipDiff{
			ToAdd:    toAdd,
			ToRemove: toRemove,
			ToKeep:   toKeep,
		},
	}
}

func (e *Engine) Sync(ctx context.Context) error {
	start := time.Now()
	defer func() {
		metrics.SyncDuration.Observe(time.Since(start).Seconds())
	}()

	// Ensure we have a fresh Keycloak session for this sync run
	e.keycloak.ResetToken()
	
	info, err := e.FetchCommonInfo(ctx)
	if err != nil {
		return err
	}

	orgPlan := e.BuildOrgSyncPlan(info)

	// Handle Disabled Users
	for username, ident := range info.KeycloakIdentities {
		userID := info.KeycloakUserIDByUsername[username]
		if !info.KeycloakUserEnabled[userID] {
			log.Printf("Handling disabled user %s", username)
			_ = e.keycloak.RemoveUserFederatedIdentity(ctx, userID, e.config.Keycloak.Provider)

			login := info.ResolvedLoginsByUserID[ident.UserID]
			_ = e.discord.SendReport(e.config.Discord.LogChannelID, fmt.Sprintf(":warning: Keycloak user `%s` (GitHub: `%s`) is disabled. Removing the Keycloak GitHub link and removing from GitHub org.", username, login))

			if discordID, ok := info.KeycloakDiscordIDs[userID]; ok {
				msg := "Your GitHub organization access has been revoked because your Keycloak account is disabled."
				_ = e.discord.SendDM(discordID, msg)
			}
		}
	}

	// Handle Failed Invites
	for _, login := range orgPlan.SkippedFailed {
		log.Printf("Handling failed invite for %s", login)
		_ = e.github.RemoveFailedInvitation(ctx, login)
		_ = e.discord.SendReport(e.config.Discord.LogChannelID, fmt.Sprintf(":warning: GitHub org invite failed for `%s` because a failed invitation record exists. Removing the Keycloak GitHub link and notifying the user to reconnect.", login))

		// Find Keycloak user
		var kcUserID string
		for user, ident := range info.KeycloakIdentities {
			if strings.EqualFold(info.ResolvedLoginsByUserID[ident.UserID], login) {
				kcUserID = info.KeycloakUserIDByUsername[user]
				break
			}
		}

		if kcUserID != "" {
			_ = e.keycloak.RemoveUserFederatedIdentity(ctx, kcUserID, e.config.Keycloak.Provider)
			if discordID, ok := info.KeycloakDiscordIDs[kcUserID]; ok {
				msg := fmt.Sprintf("Your GitHub organisation invite expired/was not accepted. It will not be retried automatically.\n\nPlease reconnect your GitHub account in Keycloak and we will try again:\n\n%s", e.getKeycloakAccountLink())
				_ = e.discord.SendDM(discordID, msg)
			}
		}
	}

	// Apply Org Additions
	var orgAdded []string
	for _, login := range orgPlan.Diff.ToAdd {
		// Find userID for this login
		var inviteeID int64
		var kcUserID string
		for user, ident := range info.KeycloakIdentities {
			if strings.EqualFold(info.ResolvedLoginsByUserID[ident.UserID], login) {
				idInt, _ := strconv.ParseInt(ident.UserID, 10, 64)
				inviteeID = idInt
				kcUserID = info.KeycloakUserIDByUsername[user]
				break
			}
		}

		if inviteeID == 0 {
			log.Printf("Warning: Could not find GitHub UserID for %s to invite", login)
			continue
		}

		err := e.github.AddOrgMember(ctx, inviteeID)
		if err == nil {
			orgAdded = append(orgAdded, login)
			metrics.OrgAddedTotal.Inc()

			// Send DM
			if discordID, ok := info.KeycloakDiscordIDs[kcUserID]; ok {
				msg := fmt.Sprintf("You've been invited to join the python-discord GitHub organization!\n\nAccept your invitation here: %s", e.getGithubInviteLink())
				_ = e.discord.SendDM(discordID, msg)
			}

		} else {
			log.Printf("Error adding %s (ID %d) to org: %v", login, inviteeID, err)
		}
	}

	// Apply Org Removals
	var orgRemoved []string
	for _, login := range orgPlan.Diff.ToRemove {
		err := e.github.RemoveOrgMember(ctx, login)
		if err == nil {
			orgRemoved = append(orgRemoved, login)
			metrics.OrgRemovedTotal.Inc()
		} else {
			log.Printf("Error removing %s from org: %v", login, err)
		}
	}

	// Build mapping from KC username to GitHub Login
	kcToGithub := make(map[string]string)
	ignored := e.getIgnoredUsers()
	for user, ident := range info.KeycloakIdentities {
		userID := info.KeycloakUserIDByUsername[user]
		if !info.KeycloakUserEnabled[userID] {
			continue
		}
		login := info.ResolvedLoginsByUserID[ident.UserID]
		if _, ok := info.GitHubOrgMembersByID[ident.UserID]; ok {
			if _, ok := ignored[strings.ToLower(login)]; !ok {
				kcToGithub[user] = login
			}
		}
	}

	orgUsersToRemoveNorm := make(map[string]struct{})
	for _, u := range orgPlan.Diff.ToRemove {
		orgUsersToRemoveNorm[strings.ToLower(u)] = struct{}{}
	}

	// Team Sync
	var teamAdded []string
	var teamRemoved []string

	for role, mapping := range e.config.Mappings {
		// Find Keycloak users with this role
		var desired []string
		for userID, roles := range info.KeycloakUserRoles {
			hasRole := false
			for _, r := range roles {
				if r == role {
					hasRole = true
					break
				}
			}
			if hasRole {
				// Find username for this userID
				var username string
				for u, id := range info.KeycloakUserIDByUsername {
					if id == userID {
						username = u
						break
					}
				}
				if login, ok := kcToGithub[username]; ok {
					desired = append(desired, login)
				}
			}
		}

		current, err := e.github.ListTeamMembers(ctx, mapping.GithubTeamSlug)
		if err != nil {
			return fmt.Errorf("error listing team members for %s: %w", mapping.GithubTeamSlug, err)
		}
		metrics.TeamMembersTotal.WithLabelValues(mapping.GithubTeamSlug).Set(float64(len(current)))

		plan := e.BuildTeamSyncPlan(mapping.GithubTeamSlug, desired, current, orgUsersToRemoveNorm)

		for _, login := range plan.Diff.ToAdd {
			err := e.github.AddTeamMember(ctx, mapping.GithubTeamSlug, login)
			if err == nil {
				teamAdded = append(teamAdded, fmt.Sprintf("%s -> %s", login, mapping.GithubTeamSlug))
				metrics.TeamAddedTotal.WithLabelValues(mapping.GithubTeamSlug).Inc()
			} else {
				log.Printf("Error adding %s to team %s: %v", login, mapping.GithubTeamSlug, err)
			}
		}

		for _, login := range plan.Diff.ToRemove {
			err := e.github.RemoveTeamMember(ctx, mapping.GithubTeamSlug, login)
			if err == nil {
				teamRemoved = append(teamRemoved, fmt.Sprintf("%s -> %s", login, mapping.GithubTeamSlug))
				metrics.TeamRemovedTotal.WithLabelValues(mapping.GithubTeamSlug).Inc()
			} else {
				log.Printf("Error removing %s from team %s: %v", login, mapping.GithubTeamSlug, err)
			}
		}
	}

	// Report
	if len(orgAdded) > 0 || len(orgRemoved) > 0 || len(teamAdded) > 0 || len(teamRemoved) > 0 {
		report := ":white_check_mark: **GitHub membership sync complete**\n"
		report += fmt.Sprintf(":office: Org added: %s\n", strings.Join(formatList(orgAdded), ", "))
		report += fmt.Sprintf(":office: Org removed: %s\n", strings.Join(formatList(orgRemoved), ", "))
		report += fmt.Sprintf(":busts_in_silhouette: Team added: %s\n", strings.Join(formatList(teamAdded), ", "))
		report += fmt.Sprintf(":busts_in_silhouette: Team removed: %s\n", strings.Join(formatList(teamRemoved), ", "))

		_ = e.discord.SendReport(e.config.Discord.DebugThreadID, report)
	}

	return nil
}

func formatList(l []string) []string {
	if len(l) == 0 {
		return []string{"none"}
	}
	res := make([]string, len(l))
	for i, s := range l {
		res[i] = fmt.Sprintf("`%s`", s)
	}
	return res
}
