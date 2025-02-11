// Copyright 2024 Daytona Platforms Inc.
// SPDX-License-Identifier: Apache-2.0

package docker

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/daytonaio/daytona/pkg/workspace/project"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	log "github.com/sirupsen/logrus"
)

// pulledImages map keeps track of pulled images for project creation in order to avoid pulling the same image multiple times
// This is only an optimisation for images with tag 'latest'
func (d *DockerClient) createProjectFromImage(opts *CreateProjectOptions, pulledImages map[string]bool) error {
	if pulledImages[opts.Project.Image] {
		return d.initProjectContainer(opts)
	}

	err := d.PullImage(opts.Project.Image, opts.ContainerRegistry, opts.LogWriter)
	if err != nil {
		return err
	}
	pulledImages[opts.Project.Image] = true

	return d.initProjectContainer(opts)
}

func (d *DockerClient) initProjectContainer(opts *CreateProjectOptions) error {
	ctx := context.Background()

	c, err := d.apiClient.ContainerCreate(ctx, GetContainerCreateConfig(opts.Project), &container.HostConfig{
		Privileged: true,
		Mounts: []mount.Mount{
			{
				Type:   mount.TypeBind,
				Source: opts.ProjectDir,
				Target: fmt.Sprintf("/home/%s/%s", opts.Project.User, opts.Project.Name),
			},
		},
		ExtraHosts: []string{
			"host.docker.internal:host-gateway",
		},
	}, nil, nil, d.GetProjectContainerName(opts.Project))
	if err != nil {
		return err
	}

	err = d.apiClient.ContainerStart(ctx, c.ID, container.StartOptions{})
	if err != nil {
		return err
	}

	go func() {
		for {
			err = d.GetContainerLogs(c.ID, opts.LogWriter)
			if err == nil {
				break
			}
			log.Error(err)
			time.Sleep(100 * time.Millisecond)
		}
	}()

	if runtime.GOOS != "windows" {
		_, err = d.updateContainerUserUidGid(c.ID, opts)
	}

	err = d.apiClient.ContainerStop(ctx, c.ID, container.StopOptions{})
	if err != nil {
		return err
	}

	return nil
}

func GetContainerCreateConfig(project *project.Project) *container.Config {
	envVars := []string{}

	for key, value := range project.EnvVars {
		envVars = append(envVars, fmt.Sprintf("%s=%s", key, value))
	}

	return &container.Config{
		Hostname: project.Name,
		Image:    project.Image,
		Labels: map[string]string{
			"daytona.workspace.id":                     project.WorkspaceId,
			"daytona.workspace.project.name":           project.Name,
			"daytona.workspace.project.repository.url": project.Repository.Url,
		},
		User:         project.User,
		Env:          envVars,
		Entrypoint:   []string{"sleep", "infinity"},
		AttachStdout: true,
		AttachStderr: true,
	}
}
