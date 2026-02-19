package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	appconfig "github.com/CharlieAIO/sol-cloud/internal/config"
	"os/exec"
	"strings"
)

type railwayGraphQLResponse struct {
	Data   map[string]json.RawMessage `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

func railwayGraphQLRequest(ctx context.Context, client *http.Client, url, token, query string, variables map[string]any) (*railwayGraphQLResponse, error) {
	payload := map[string]any{
		"query": query,
	}
	if variables != nil {
		payload["variables"] = variables
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal graphql request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build graphql request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("graphql request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024*1024))
	if err != nil {
		return nil, fmt.Errorf("read graphql response: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("railway auth rejected (%d): %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("railway graphql status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var decoded railwayGraphQLResponse
	if err := json.Unmarshal(respBody, &decoded); err != nil {
		return nil, fmt.Errorf("decode graphql response: %w", err)
	}
	return &decoded, nil
}

func ensureRailwayInstalled() error {
	if _, err := exec.LookPath("railway"); err != nil {
		return fmt.Errorf("railway CLI not found in PATH: %w (install from https://docs.railway.app/guides/cli)", err)
	}
	return nil
}

// RailwayWorkspace is a Railway workspace (team).
type RailwayWorkspace struct {
	ID   string
	Name string
}

// ListRailwayWorkspaces attempts to discover the workspace for the given token.
// It tries multiple query shapes because Railway's public API has changed over time.
// Returns a non-empty slice on success, nil slice when nothing could be found (not an error).
func ListRailwayWorkspaces(ctx context.Context, client *http.Client, graphqlURL, token string) ([]RailwayWorkspace, error) {
	// Attempt 1: me { teams { id name } } — Railway's internal term is still "teams".
	if ws, ok := tryWorkspacesViaMeField(ctx, client, graphqlURL, token, "teams"); ok {
		return ws, nil
	}

	// Attempt 2: me { workspaces { id name } } — in case Railway renamed it.
	if ws, ok := tryWorkspacesViaMeField(ctx, client, graphqlURL, token, "workspaces"); ok {
		return ws, nil
	}

	// Attempt 3: extract teamId from the first project (works when projects exist).
	if ws, ok := tryWorkspaceViaProjects(ctx, client, graphqlURL, token); ok {
		return ws, nil
	}

	// Nothing worked — caller decides what to do (prompt user, skip, etc.).
	return nil, nil
}

func tryWorkspacesViaMeField(ctx context.Context, client *http.Client, graphqlURL, token, field string) ([]RailwayWorkspace, bool) {
	query := fmt.Sprintf(`query { me { %s { id name } } }`, field)
	resp, err := railwayGraphQLRequest(ctx, client, graphqlURL, token, query, nil)
	if err != nil || len(resp.Errors) > 0 {
		return nil, false
	}
	var me map[string][]struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if data, ok := resp.Data["me"]; ok {
		_ = json.Unmarshal(data, &me)
	}
	items := me[field]
	if len(items) == 0 {
		return nil, false
	}
	out := make([]RailwayWorkspace, len(items))
	for i, w := range items {
		out[i] = RailwayWorkspace{ID: w.ID, Name: w.Name}
	}
	return out, true
}

func tryWorkspaceViaProjects(ctx context.Context, client *http.Client, graphqlURL, token string) ([]RailwayWorkspace, bool) {
	resp, err := railwayGraphQLRequest(ctx, client, graphqlURL, token,
		`query { projects { edges { node { id name teamId } } } }`, nil)
	if err != nil || len(resp.Errors) > 0 {
		return nil, false
	}
	var projects struct {
		Edges []struct {
			Node struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				TeamID string `json:"teamId"`
			} `json:"node"`
		} `json:"edges"`
	}
	if data, ok := resp.Data["projects"]; ok {
		_ = json.Unmarshal(data, &projects)
	}
	for _, edge := range projects.Edges {
		if edge.Node.TeamID != "" {
			return []RailwayWorkspace{{ID: edge.Node.TeamID}}, true
		}
	}
	return nil, false
}

// resolveRailwayWorkspaceID returns the workspace ID to use for project creation.
// Priority: explicit orgSlug > savedWorkspaceID from credentials > API query.
func resolveRailwayWorkspaceID(ctx context.Context, client *http.Client, graphqlURL, token, orgSlug, savedWorkspaceID string) (string, error) {
	if id := strings.TrimSpace(orgSlug); id != "" {
		return id, nil
	}
	if id := strings.TrimSpace(savedWorkspaceID); id != "" {
		return id, nil
	}

	// Last resort: query the API.
	workspaces, err := ListRailwayWorkspaces(ctx, client, graphqlURL, token)
	if err != nil {
		return "", err
	}
	if len(workspaces) == 0 {
		return "", fmt.Errorf("could not resolve Railway workspace; run `sol-cloud auth railway` to set up credentials")
	}
	return workspaces[0].ID, nil
}

// ensureRailwayProject returns the project ID for the named project, creating it if needed.
func ensureRailwayProject(ctx context.Context, client *http.Client, graphqlURL, token, name, orgSlug, savedWorkspaceID string) (string, error) {
	// Try to find existing project by listing projects.
	listQuery := `query { projects { edges { node { id name } } } }`
	listResp, err := railwayGraphQLRequest(ctx, client, graphqlURL, token, listQuery, nil)
	if err != nil {
		return "", fmt.Errorf("list railway projects: %w", err)
	}
	if len(listResp.Errors) > 0 {
		return "", fmt.Errorf("list railway projects error: %s", listResp.Errors[0].Message)
	}

	type projectNode struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type projectEdge struct {
		Node projectNode `json:"node"`
	}
	type projectsData struct {
		Edges []projectEdge `json:"edges"`
	}
	var projects struct {
		Projects projectsData `json:"projects"`
	}
	if raw, ok := listResp.Data["projects"]; ok {
		_ = json.Unmarshal(raw, &projects.Projects)
	}
	for _, edge := range projects.Projects.Edges {
		if edge.Node.Name == name {
			return edge.Node.ID, nil
		}
	}

	// Project not found — resolve the workspace ID then create it.
	workspaceID, err := resolveRailwayWorkspaceID(ctx, client, graphqlURL, token, orgSlug, savedWorkspaceID)
	if err != nil {
		return "", err
	}

	createInput := map[string]any{
		"name":        name,
		"workspaceId": workspaceID,
	}
	createQuery := `mutation ProjectCreate($input: ProjectCreateInput!) { projectCreate(input: $input) { id } }`
	createResp, err := railwayGraphQLRequest(ctx, client, graphqlURL, token, createQuery, map[string]any{"input": createInput})
	if err != nil {
		return "", fmt.Errorf("create railway project: %w", err)
	}
	if len(createResp.Errors) > 0 {
		return "", fmt.Errorf("create railway project error: %s", createResp.Errors[0].Message)
	}

	var createResult struct {
		ProjectCreate struct {
			ID string `json:"id"`
		} `json:"projectCreate"`
	}
	if raw, ok := createResp.Data["projectCreate"]; ok {
		if err := json.Unmarshal(raw, &createResult.ProjectCreate); err != nil {
			return "", fmt.Errorf("decode project create response: %w", err)
		}
	}
	if createResult.ProjectCreate.ID == "" {
		return "", fmt.Errorf("railway project create returned empty id")
	}
	return createResult.ProjectCreate.ID, nil
}

// ensureRailwayService returns the service ID for the named service in a project, creating it if needed.
func ensureRailwayService(ctx context.Context, client *http.Client, graphqlURL, token, projectID, serviceName string) (string, error) {
	// Check existing services.
	getQuery := `query Project($id: String!) { project(id: $id) { services { edges { node { id name } } } } }`
	getResp, err := railwayGraphQLRequest(ctx, client, graphqlURL, token, getQuery, map[string]any{"id": projectID})
	if err != nil {
		return "", fmt.Errorf("get railway project services: %w", err)
	}
	if len(getResp.Errors) > 0 {
		return "", fmt.Errorf("get railway project services error: %s", getResp.Errors[0].Message)
	}

	type serviceNode struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type serviceEdge struct {
		Node serviceNode `json:"node"`
	}
	type servicesData struct {
		Edges []serviceEdge `json:"edges"`
	}
	var project struct {
		Project struct {
			Services servicesData `json:"services"`
		} `json:"project"`
	}
	if raw, ok := getResp.Data["project"]; ok {
		_ = json.Unmarshal(raw, &project.Project)
	}
	for _, edge := range project.Project.Services.Edges {
		if edge.Node.Name == serviceName {
			return edge.Node.ID, nil
		}
	}

	// Create service.
	createQuery := `mutation ServiceCreate($input: ServiceCreateInput!) { serviceCreate(input: $input) { id } }`
	createResp, err := railwayGraphQLRequest(ctx, client, graphqlURL, token, createQuery, map[string]any{
		"input": map[string]any{
			"projectId": projectID,
			"name":      serviceName,
		},
	})
	if err != nil {
		return "", fmt.Errorf("create railway service: %w", err)
	}
	if len(createResp.Errors) > 0 {
		return "", fmt.Errorf("create railway service error: %s", createResp.Errors[0].Message)
	}

	var createResult struct {
		ServiceCreate struct {
			ID string `json:"id"`
		} `json:"serviceCreate"`
	}
	if raw, ok := createResp.Data["serviceCreate"]; ok {
		if err := json.Unmarshal(raw, &createResult.ServiceCreate); err != nil {
			return "", fmt.Errorf("decode service create response: %w", err)
		}
	}
	if createResult.ServiceCreate.ID == "" {
		return "", fmt.Errorf("railway service create returned empty id")
	}
	return createResult.ServiceCreate.ID, nil
}

// resolveRailwayEnvironmentID returns the default environment ID for a project.
func resolveRailwayEnvironmentID(ctx context.Context, client *http.Client, graphqlURL, token, projectID string) (string, error) {
	query := `query Project($id: String!) { project(id: $id) { environments { edges { node { id name } } } } }`
	resp, err := railwayGraphQLRequest(ctx, client, graphqlURL, token, query, map[string]any{"id": projectID})
	if err != nil {
		return "", fmt.Errorf("get railway environments: %w", err)
	}
	if len(resp.Errors) > 0 {
		return "", fmt.Errorf("get railway environments error: %s", resp.Errors[0].Message)
	}

	type envNode struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	type envEdge struct {
		Node envNode `json:"node"`
	}
	var project struct {
		Environments struct {
			Edges []envEdge `json:"edges"`
		} `json:"environments"`
	}
	if raw, ok := resp.Data["project"]; ok {
		_ = json.Unmarshal(raw, &project)
	}
	// Return the first environment (typically "production").
	for _, edge := range project.Environments.Edges {
		if strings.ToLower(edge.Node.Name) == "production" {
			return edge.Node.ID, nil
		}
	}
	if len(project.Environments.Edges) > 0 {
		return project.Environments.Edges[0].Node.ID, nil
	}
	return "", fmt.Errorf("no environments found for railway project %s", projectID)
}

// fetchRailwayDomain returns the existing Railway-generated domain for a service, or "".
func fetchRailwayDomain(ctx context.Context, client *http.Client, graphqlURL, token, projectID, serviceID, environmentID string) string {
	query := `query ServiceDomains($projectId: String!, $serviceId: String!, $environmentId: String!) {
		domains(projectId: $projectId, serviceId: $serviceId, environmentId: $environmentId) {
			serviceDomains { domain }
		}
	}`
	resp, err := railwayGraphQLRequest(ctx, client, graphqlURL, token, query, map[string]any{
		"projectId":     projectID,
		"serviceId":     serviceID,
		"environmentId": environmentID,
	})
	if err != nil || len(resp.Errors) > 0 {
		return ""
	}
	var result struct {
		ServiceDomains []struct {
			Domain string `json:"domain"`
		} `json:"serviceDomains"`
	}
	if raw, ok := resp.Data["domains"]; ok {
		_ = json.Unmarshal(raw, &result)
	}
	for _, sd := range result.ServiceDomains {
		if d := strings.TrimSpace(sd.Domain); d != "" {
			return d
		}
	}
	return ""
}

// enableRailwayPublicNetworking ensures the service has a Railway-generated public domain.
// Returns the domain on success.
func enableRailwayPublicNetworking(ctx context.Context, client *http.Client, graphqlURL, token, projectID, serviceID, environmentID string) (string, error) {
	// Return an existing domain if one already exists.
	if d := fetchRailwayDomain(ctx, client, graphqlURL, token, projectID, serviceID, environmentID); d != "" {
		return d, nil
	}

	// Create a Railway-generated domain.
	// Note: projectId is NOT included — the service already implies the project.
	createResp, err := railwayGraphQLRequest(ctx, client, graphqlURL, token,
		`mutation ServiceDomainCreate($input: ServiceDomainCreateInput!) {
			serviceDomainCreate(input: $input) { domain }
		}`,
		map[string]any{
			"input": map[string]any{
				"serviceId":     serviceID,
				"environmentId": environmentID,
			},
		})
	if err != nil {
		return "", fmt.Errorf("create railway domain: %w", err)
	}
	if len(createResp.Errors) > 0 {
		msg := strings.ToLower(createResp.Errors[0].Message)
		if strings.Contains(msg, "already") {
			// Domain already exists — fetch and return it.
			if d := fetchRailwayDomain(ctx, client, graphqlURL, token, projectID, serviceID, environmentID); d != "" {
				return d, nil
			}
			return "", nil
		}
		return "", fmt.Errorf("create railway domain: %s", createResp.Errors[0].Message)
	}

	var result struct {
		Domain string `json:"domain"`
	}
	if raw, ok := createResp.Data["serviceDomainCreate"]; ok {
		_ = json.Unmarshal(raw, &result)
	}
	return strings.TrimSpace(result.Domain), nil
}

// ensureRailwayVolume creates a persistent volume attached to the service at the ledger mount path.
// VolumeCreateInput schema (confirmed via introspection):
//
//	projectId: String!  mountPath: String!  serviceId: String  environmentId: String  region: String
//
// There is no sizeMB field — Railway sets a default size, resizable from the dashboard.
func ensureRailwayVolume(ctx context.Context, client *http.Client, graphqlURL, token, projectID, serviceID, environmentID string) (string, error) {
	const mountPath = "/var/lib/solana/ledger"

	// Check if a volume is already attached to this service.
	checkResp, err := railwayGraphQLRequest(ctx, client, graphqlURL, token,
		`query Project($id: String!) {
			project(id: $id) {
				volumes {
					edges {
						node {
							volumeInstances {
								edges { node { serviceId } }
							}
						}
					}
				}
			}
		}`, map[string]any{"id": projectID})
	if err == nil && len(checkResp.Errors) == 0 {
		var proj struct {
			Volumes struct {
				Edges []struct {
					Node struct {
						VolumeInstances struct {
							Edges []struct {
								Node struct {
									ServiceID string `json:"serviceId"`
								} `json:"node"`
							} `json:"edges"`
						} `json:"volumeInstances"`
					} `json:"node"`
				} `json:"edges"`
			} `json:"volumes"`
		}
		if raw, ok := checkResp.Data["project"]; ok {
			_ = json.Unmarshal(raw, &proj)
		}
		for _, ve := range proj.Volumes.Edges {
			for _, ie := range ve.Node.VolumeInstances.Edges {
				if ie.Node.ServiceID == serviceID {
					return "volume already attached\n", nil
				}
			}
		}
	}

	// Create and attach the volume in a single call.
	// mountPath and projectId are required; serviceId and environmentId attach it immediately.
	createResp, err := railwayGraphQLRequest(ctx, client, graphqlURL, token,
		`mutation VolumeCreate($input: VolumeCreateInput!) {
			volumeCreate(input: $input) { id }
		}`,
		map[string]any{
			"input": map[string]any{
				"projectId":     projectID,
				"mountPath":     mountPath,
				"serviceId":     serviceID,
				"environmentId": environmentID,
			},
		})
	if err != nil {
		return "", fmt.Errorf("create volume: %w", err)
	}
	if len(createResp.Errors) > 0 {
		msg := strings.ToLower(createResp.Errors[0].Message)
		if strings.Contains(msg, "already") || strings.Contains(msg, "exist") {
			return "volume already exists\n", nil
		}
		return "", fmt.Errorf("create volume: %s", createResp.Errors[0].Message)
	}

	var result struct {
		ID string `json:"id"`
	}
	if raw, ok := createResp.Data["volumeCreate"]; ok {
		_ = json.Unmarshal(raw, &result)
	}
	if result.ID == "" {
		return "", fmt.Errorf("volumeCreate returned empty id")
	}
	return fmt.Sprintf("volume created: %s at %s\n", result.ID, mountPath), nil
}

// createRailwayProjectToken creates a project-scoped token for use with the Railway CLI.
// The Railway CLI (railway up) requires a project token, not an account-level API token.
func createRailwayProjectToken(ctx context.Context, client *http.Client, graphqlURL, accountToken, projectID, environmentID string) (string, error) {
	mutation := `mutation ProjectTokenCreate($input: ProjectTokenCreateInput!) {
		projectTokenCreate(input: $input)
	}`
	resp, err := railwayGraphQLRequest(ctx, client, graphqlURL, accountToken, mutation, map[string]any{
		"input": map[string]any{
			"projectId":     projectID,
			"environmentId": environmentID,
			"name":          "sol-cloud-deploy",
		},
	})
	if err != nil {
		return "", fmt.Errorf("create project token: %w", err)
	}
	if len(resp.Errors) > 0 {
		return "", fmt.Errorf("create project token error: %s", resp.Errors[0].Message)
	}
	var projectToken string
	if raw, ok := resp.Data["projectTokenCreate"]; ok {
		if err := json.Unmarshal(raw, &projectToken); err != nil {
			return "", fmt.Errorf("decode project token response: %w", err)
		}
	}
	if projectToken == "" {
		return "", fmt.Errorf("project token create returned empty token")
	}
	return projectToken, nil
}

func deployViaRailwayCLI(ctx context.Context, token string, cfg *Config, artifactsDir string) (logs, domain, projectID, serviceID string, err error) {
	if err := ensureRailwayInstalled(); err != nil {
		return "", "", "", "", err
	}

	client := &http.Client{Timeout: defaultRailwayHTTPTimeout}
	graphqlURL := defaultRailwayGraphQLURL

	var logBuilder strings.Builder
	logBuilder.WriteString("railway deploy started\n")
	logBuilder.WriteString(fmt.Sprintf("project=%s region=%s\n", cfg.Name, cfg.Region))

	savedWorkspaceID := ""
	if creds, credErr := appconfig.LoadCredentials(); credErr == nil {
		savedWorkspaceID = creds.Railway.WorkspaceID
	}

	projectID, err = ensureRailwayProject(ctx, client, graphqlURL, token, cfg.Name, cfg.OrgSlug, savedWorkspaceID)
	if err != nil {
		return logBuilder.String(), "", "", "", err
	}
	logBuilder.WriteString(fmt.Sprintf("project ensured: %s\n", projectID))

	serviceID, err = ensureRailwayService(ctx, client, graphqlURL, token, projectID, "validator")
	if err != nil {
		return logBuilder.String(), "", "", "", err
	}
	logBuilder.WriteString(fmt.Sprintf("service ensured: %s\n", serviceID))

	// Resolve environment then set up everything scoped to it.
	environmentID := ""
	cliToken := token
	if envID, envErr := resolveRailwayEnvironmentID(ctx, client, graphqlURL, token, projectID); envErr != nil {
		logBuilder.WriteString(fmt.Sprintf("warning: could not resolve environment id: %v\n", envErr))
	} else {
		environmentID = envID
		logBuilder.WriteString(fmt.Sprintf("environment: %s\n", environmentID))

		// Attach a 30 GB persistent volume for the ledger (account token — project token not authorized).
		volLogs, volErr := ensureRailwayVolume(ctx, client, graphqlURL, token, projectID, serviceID, environmentID)
		logBuilder.WriteString(volLogs)
		if volErr != nil {
			logBuilder.WriteString(fmt.Sprintf("warning: could not create volume: %v\n", volErr))
		} else {
			logBuilder.WriteString("volume ensured: 30 GB at /var/lib/solana/ledger\n")
		}

		// Generate a public domain for the service (account token — project token not authorized).
		domain, err = enableRailwayPublicNetworking(ctx, client, graphqlURL, token, projectID, serviceID, environmentID)
		if err != nil {
			logBuilder.WriteString(fmt.Sprintf("warning: could not create domain: %v\n", err))
			err = nil // non-fatal
		} else if domain != "" {
			logBuilder.WriteString(fmt.Sprintf("domain: %s\n", domain))
		} else {
			logBuilder.WriteString("warning: domain not yet assigned (will re-fetch after deploy)\n")
		}

		// Create project token for the CLI (railway up requires it, not account token).
		if pt, ptErr := createRailwayProjectToken(ctx, client, graphqlURL, token, projectID, environmentID); ptErr != nil {
			logBuilder.WriteString(fmt.Sprintf("warning: could not create project token (%v); using account token\n", ptErr))
		} else {
			cliToken = pt
			logBuilder.WriteString("created project token\n")
		}
	}

	env := append(os.Environ(),
		"RAILWAY_TOKEN="+cliToken,
		"RAILWAY_PROJECT_ID="+projectID,
		"RAILWAY_SERVICE_ID="+serviceID,
	)
	if environmentID != "" {
		env = append(env, "RAILWAY_ENVIRONMENT_ID="+environmentID)
	}
	deployOutput, deployErr := runCommandWithEnv(ctx, artifactsDir, "", env, "railway", "up", "--ci", "--service", serviceID)
	logBuilder.WriteString("\n[railway up]\n")
	logBuilder.WriteString(deployOutput)
	if deployErr != nil {
		return logBuilder.String(), "", "", "", commandStageError("railway deploy", deployErr, deployOutput)
	}

	// If domain wasn't set before deployment, re-query now that a deployment exists.
	if domain == "" && environmentID != "" {
		if d, dErr := enableRailwayPublicNetworking(ctx, client, graphqlURL, token, projectID, serviceID, environmentID); dErr == nil && d != "" {
			domain = d
			logBuilder.WriteString(fmt.Sprintf("domain (post-deploy): %s\n", domain))
		}
	}

	return logBuilder.String(), domain, projectID, serviceID, nil
}

func destroyRailwayProject(ctx context.Context, client *http.Client, graphqlURL, token, projectID string) error {
	mutation := `mutation ProjectDelete($id: String!) { projectDelete(id: $id) }`
	resp, err := railwayGraphQLRequest(ctx, client, graphqlURL, token, mutation, map[string]any{"id": projectID})
	if err != nil {
		return err
	}
	if len(resp.Errors) > 0 {
		msg := strings.ToLower(resp.Errors[0].Message)
		if strings.Contains(msg, "not found") || strings.Contains(msg, "does not exist") {
			return nil
		}
		return fmt.Errorf("delete railway project error: %s", resp.Errors[0].Message)
	}
	return nil
}

func getRailwayServiceStatus(ctx context.Context, client *http.Client, graphqlURL, token, projectID, serviceID string) (string, error) {
	query := `query Deployments($input: DeploymentListInput!) {
		deployments(input: $input) {
			edges {
				node {
					status
				}
			}
		}
	}`
	resp, err := railwayGraphQLRequest(ctx, client, graphqlURL, token, query, map[string]any{
		"input": map[string]any{
			"projectId": projectID,
			"serviceId": serviceID,
		},
	})
	if err != nil {
		return "unknown", err
	}
	if len(resp.Errors) > 0 {
		return "unknown", fmt.Errorf("get railway status error: %s", resp.Errors[0].Message)
	}

	type deploymentNode struct {
		Status string `json:"status"`
	}
	type deploymentEdge struct {
		Node deploymentNode `json:"node"`
	}
	var deploymentsResult struct {
		Edges []deploymentEdge `json:"edges"`
	}
	if raw, ok := resp.Data["deployments"]; ok {
		_ = json.Unmarshal(raw, &deploymentsResult)
	}

	if len(deploymentsResult.Edges) == 0 {
		return "stopped", nil
	}

	// Return the status of the most recent deployment.
	status := strings.ToLower(strings.TrimSpace(deploymentsResult.Edges[0].Node.Status))
	switch status {
	case "success":
		return "running", nil
	case "deploying", "building":
		return "starting", nil
	case "failed", "crashed":
		return "stopped", nil
	default:
		if status == "" {
			return "unknown", nil
		}
		return status, nil
	}
}

func restartRailwayService(ctx context.Context, client *http.Client, graphqlURL, token, projectID, serviceID string) error {
	// Railway uses serviceInstanceRedeploy which requires environmentId.
	// First get the environment ID.
	envID, err := resolveRailwayEnvironmentID(ctx, client, graphqlURL, token, projectID)
	if err != nil {
		return fmt.Errorf("resolve environment for restart: %w", err)
	}

	mutation := `mutation ServiceInstanceRedeploy($environmentId: String!, $serviceId: String!) {
		serviceInstanceRedeploy(environmentId: $environmentId, serviceId: $serviceId)
	}`
	resp, err := railwayGraphQLRequest(ctx, client, graphqlURL, token, mutation, map[string]any{
		"environmentId": envID,
		"serviceId":     serviceID,
	})
	if err != nil {
		return err
	}
	if len(resp.Errors) > 0 {
		return fmt.Errorf("restart railway service error: %s", resp.Errors[0].Message)
	}
	return nil
}
