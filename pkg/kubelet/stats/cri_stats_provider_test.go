/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package stats

import (
	"math/rand"
	"testing"
	"time"

	cadvisorfs "github.com/google/cadvisor/fs"
	"github.com/stretchr/testify/assert"

	cadvisorapiv2 "github.com/google/cadvisor/info/v2"
	critest "k8s.io/kubernetes/pkg/kubelet/apis/cri/testing"
	runtimeapi "k8s.io/kubernetes/pkg/kubelet/apis/cri/v1alpha1/runtime"
	statsapi "k8s.io/kubernetes/pkg/kubelet/apis/stats/v1alpha1"
	cadvisortest "k8s.io/kubernetes/pkg/kubelet/cadvisor/testing"
	kubecontainertest "k8s.io/kubernetes/pkg/kubelet/container/testing"
	kubepodtest "k8s.io/kubernetes/pkg/kubelet/pod/testing"
)

func TestCRIListPodStats(t *testing.T) {
	var (
		imageFsStorageUUID = "imagefs-storage-uuid"
		unknownStorageUUID = "unknown-storage-uuid"
		imageFsInfo        = getTestFsInfo(2000)
		rootFsInfo         = getTestFsInfo(1000)

		sandbox0        = makeFakePodSandbox("sandbox0-name", "sandbox0-uid", "sandbox0-ns")
		container0      = makeFakeContainer(sandbox0, "container0-name", 0, false)
		containerStats0 = makeFakeContainerStats(container0, imageFsStorageUUID)
		container1      = makeFakeContainer(sandbox0, "container1-name", 0, false)
		containerStats1 = makeFakeContainerStats(container1, unknownStorageUUID)

		sandbox1        = makeFakePodSandbox("sandbox1-name", "sandbox1-uid", "sandbox1-ns")
		container2      = makeFakeContainer(sandbox1, "container2-name", 0, false)
		containerStats2 = makeFakeContainerStats(container2, imageFsStorageUUID)

		sandbox2        = makeFakePodSandbox("sandbox2-name", "sandbox2-uid", "sandbox2-ns")
		container3      = makeFakeContainer(sandbox2, "container3-name", 0, true)
		containerStats3 = makeFakeContainerStats(container3, imageFsStorageUUID)
		container4      = makeFakeContainer(sandbox2, "container3-name", 1, false)
		containerStats4 = makeFakeContainerStats(container4, imageFsStorageUUID)
	)

	var (
		mockCadvisor       = new(cadvisortest.Mock)
		mockRuntimeCache   = new(kubecontainertest.MockRuntimeCache)
		mockPodManager     = new(kubepodtest.MockManager)
		resourceAnalyzer   = new(fakeResourceAnalyzer)
		fakeRuntimeService = critest.NewFakeRuntimeService()
		fakeImageService   = critest.NewFakeImageService()
	)

	mockCadvisor.
		On("RootFsInfo").Return(rootFsInfo, nil).
		On("GetFsInfoByFsUUID", imageFsStorageUUID).Return(imageFsInfo, nil).
		On("GetFsInfoByFsUUID", unknownStorageUUID).Return(cadvisorapiv2.FsInfo{}, cadvisorfs.ErrNoSuchDevice)
	fakeRuntimeService.SetFakeSandboxes([]*critest.FakePodSandbox{
		sandbox0, sandbox1, sandbox2,
	})
	fakeRuntimeService.SetFakeContainers([]*critest.FakeContainer{
		container0, container1, container2, container3, container4,
	})
	fakeRuntimeService.SetFakeContainerStats([]*runtimeapi.ContainerStats{
		containerStats0, containerStats1, containerStats2, containerStats3, containerStats4,
	})

	provider := NewCRIStatsProvider(
		mockCadvisor,
		resourceAnalyzer,
		mockPodManager,
		mockRuntimeCache,
		fakeRuntimeService,
		fakeImageService)

	stats, err := provider.ListPodStats()
	assert := assert.New(t)
	assert.NoError(err)
	assert.Equal(3, len(stats))

	podStatsMap := make(map[statsapi.PodReference]statsapi.PodStats)
	for _, s := range stats {
		podStatsMap[s.PodRef] = s
	}

	p0 := podStatsMap[statsapi.PodReference{Name: "sandbox0-name", UID: "sandbox0-uid", Namespace: "sandbox0-ns"}]
	assert.Equal(sandbox0.CreatedAt, p0.StartTime.UnixNano())
	assert.Equal(2, len(p0.Containers))

	containerStatsMap := make(map[string]statsapi.ContainerStats)
	for _, s := range p0.Containers {
		containerStatsMap[s.Name] = s
	}
	c0 := containerStatsMap["container0-name"]
	assert.Equal(container0.CreatedAt, c0.StartTime.UnixNano())
	checkCRICPUAndMemoryStats(assert, c0, containerStats0)
	checkCRIRootfsStats(assert, c0, containerStats0, &imageFsInfo)
	checkCRILogsStats(assert, c0, &rootFsInfo)
	c1 := containerStatsMap["container1-name"]
	assert.Equal(container1.CreatedAt, c1.StartTime.UnixNano())
	checkCRICPUAndMemoryStats(assert, c1, containerStats1)
	checkCRIRootfsStats(assert, c1, containerStats1, nil)
	checkCRILogsStats(assert, c1, &rootFsInfo)

	p1 := podStatsMap[statsapi.PodReference{Name: "sandbox1-name", UID: "sandbox1-uid", Namespace: "sandbox1-ns"}]
	assert.Equal(sandbox1.CreatedAt, p1.StartTime.UnixNano())
	assert.Equal(1, len(p1.Containers))

	c2 := p1.Containers[0]
	assert.Equal("container2-name", c2.Name)
	assert.Equal(container2.CreatedAt, c2.StartTime.UnixNano())
	checkCRICPUAndMemoryStats(assert, c2, containerStats2)
	checkCRIRootfsStats(assert, c2, containerStats2, &imageFsInfo)
	checkCRILogsStats(assert, c2, &rootFsInfo)

	p2 := podStatsMap[statsapi.PodReference{Name: "sandbox2-name", UID: "sandbox2-uid", Namespace: "sandbox2-ns"}]
	assert.Equal(sandbox2.CreatedAt, p2.StartTime.UnixNano())
	assert.Equal(1, len(p2.Containers))

	c3 := p2.Containers[0]
	assert.Equal("container3-name", c3.Name)
	assert.Equal(container4.CreatedAt, c3.StartTime.UnixNano())
	checkCRICPUAndMemoryStats(assert, c3, containerStats4)
	checkCRIRootfsStats(assert, c3, containerStats4, &imageFsInfo)
	checkCRILogsStats(assert, c3, &rootFsInfo)

	mockCadvisor.AssertExpectations(t)
}

func TestCRIImagesFsStats(t *testing.T) {
	var (
		imageFsStorageUUID = "imagefs-storage-uuid"
		imageFsInfo        = getTestFsInfo(2000)
		imageFsUsage       = makeFakeImageFsUsage(imageFsStorageUUID)
	)
	var (
		mockCadvisor       = new(cadvisortest.Mock)
		mockRuntimeCache   = new(kubecontainertest.MockRuntimeCache)
		mockPodManager     = new(kubepodtest.MockManager)
		resourceAnalyzer   = new(fakeResourceAnalyzer)
		fakeRuntimeService = critest.NewFakeRuntimeService()
		fakeImageService   = critest.NewFakeImageService()
	)

	mockCadvisor.On("GetFsInfoByFsUUID", imageFsStorageUUID).Return(imageFsInfo, nil)
	fakeImageService.SetFakeFilesystemUsage([]*runtimeapi.FilesystemUsage{
		imageFsUsage,
	})

	provider := NewCRIStatsProvider(
		mockCadvisor,
		resourceAnalyzer,
		mockPodManager,
		mockRuntimeCache,
		fakeRuntimeService,
		fakeImageService)

	stats, err := provider.ImageFsStats()
	assert := assert.New(t)
	assert.NoError(err)

	assert.Equal(imageFsUsage.Timestamp, stats.Time.UnixNano())
	assert.Equal(imageFsInfo.Available, *stats.AvailableBytes)
	assert.Equal(imageFsInfo.Capacity, *stats.CapacityBytes)
	assert.Equal(imageFsInfo.InodesFree, stats.InodesFree)
	assert.Equal(imageFsInfo.Inodes, stats.Inodes)
	assert.Equal(imageFsUsage.UsedBytes.Value, *stats.UsedBytes)
	assert.Equal(imageFsUsage.InodesUsed.Value, *stats.InodesUsed)

	mockCadvisor.AssertExpectations(t)
}

func makeFakePodSandbox(name, uid, namespace string) *critest.FakePodSandbox {
	p := &critest.FakePodSandbox{
		PodSandboxStatus: runtimeapi.PodSandboxStatus{
			Metadata: &runtimeapi.PodSandboxMetadata{
				Name:      name,
				Uid:       uid,
				Namespace: namespace,
			},
			State:     runtimeapi.PodSandboxState_SANDBOX_READY,
			CreatedAt: time.Now().UnixNano(),
		},
	}
	p.PodSandboxStatus.Id = critest.BuildSandboxName(p.PodSandboxStatus.Metadata)
	return p
}

func makeFakeContainer(sandbox *critest.FakePodSandbox, name string, attempt uint32, terminated bool) *critest.FakeContainer {
	sandboxID := sandbox.PodSandboxStatus.Id
	c := &critest.FakeContainer{
		SandboxID: sandboxID,
		ContainerStatus: runtimeapi.ContainerStatus{
			Metadata:  &runtimeapi.ContainerMetadata{Name: name, Attempt: attempt},
			Image:     &runtimeapi.ImageSpec{},
			ImageRef:  "fake-image-ref",
			CreatedAt: time.Now().UnixNano(),
		},
	}
	c.ContainerStatus.Labels = map[string]string{
		"io.kubernetes.pod.name":       sandbox.Metadata.Name,
		"io.kubernetes.pod.uid":        sandbox.Metadata.Uid,
		"io.kubernetes.pod.namespace":  sandbox.Metadata.Namespace,
		"io.kubernetes.container.name": name,
	}
	if terminated {
		c.ContainerStatus.State = runtimeapi.ContainerState_CONTAINER_EXITED
	} else {
		c.ContainerStatus.State = runtimeapi.ContainerState_CONTAINER_RUNNING
	}
	c.ContainerStatus.Id = critest.BuildContainerName(c.ContainerStatus.Metadata, sandboxID)
	return c
}

func makeFakeContainerStats(container *critest.FakeContainer, imageFsUUID string) *runtimeapi.ContainerStats {
	containerStats := &runtimeapi.ContainerStats{
		Attributes: &runtimeapi.ContainerAttributes{
			Id:       container.ContainerStatus.Id,
			Metadata: container.ContainerStatus.Metadata,
		},
		WritableLayer: &runtimeapi.FilesystemUsage{
			Timestamp:  time.Now().UnixNano(),
			StorageId:  &runtimeapi.StorageIdentifier{Uuid: imageFsUUID},
			UsedBytes:  &runtimeapi.UInt64Value{Value: rand.Uint64()},
			InodesUsed: &runtimeapi.UInt64Value{Value: rand.Uint64()},
		},
	}
	if container.State == runtimeapi.ContainerState_CONTAINER_EXITED {
		containerStats.Cpu = nil
		containerStats.Memory = nil
	} else {
		containerStats.Cpu = &runtimeapi.CpuUsage{
			Timestamp:            time.Now().UnixNano(),
			UsageCoreNanoSeconds: &runtimeapi.UInt64Value{Value: rand.Uint64()},
		}
		containerStats.Memory = &runtimeapi.MemoryUsage{
			Timestamp:       time.Now().UnixNano(),
			WorkingSetBytes: &runtimeapi.UInt64Value{Value: rand.Uint64()},
		}
	}
	return containerStats
}

func makeFakeImageFsUsage(fsUUID string) *runtimeapi.FilesystemUsage {
	return &runtimeapi.FilesystemUsage{
		Timestamp:  time.Now().UnixNano(),
		StorageId:  &runtimeapi.StorageIdentifier{Uuid: fsUUID},
		UsedBytes:  &runtimeapi.UInt64Value{Value: rand.Uint64()},
		InodesUsed: &runtimeapi.UInt64Value{Value: rand.Uint64()},
	}
}

func checkCRICPUAndMemoryStats(assert *assert.Assertions, actual statsapi.ContainerStats, cs *runtimeapi.ContainerStats) {
	assert.Equal(cs.Cpu.Timestamp, actual.CPU.Time.UnixNano())
	assert.Equal(cs.Cpu.UsageCoreNanoSeconds.Value, *actual.CPU.UsageCoreNanoSeconds)
	assert.Zero(*actual.CPU.UsageNanoCores)

	assert.Equal(cs.Memory.Timestamp, actual.Memory.Time.UnixNano())
	assert.Nil(actual.Memory.AvailableBytes)
	assert.Nil(actual.Memory.UsageBytes)
	assert.Equal(cs.Memory.WorkingSetBytes.Value, *actual.Memory.WorkingSetBytes)
	assert.Zero(*actual.Memory.RSSBytes)
	assert.Nil(actual.Memory.PageFaults)
	assert.Nil(actual.Memory.MajorPageFaults)
}

func checkCRIRootfsStats(assert *assert.Assertions, actual statsapi.ContainerStats, cs *runtimeapi.ContainerStats, imageFsInfo *cadvisorapiv2.FsInfo) {
	assert.Equal(cs.WritableLayer.Timestamp, actual.Rootfs.Time.UnixNano())
	if imageFsInfo != nil {
		assert.Equal(imageFsInfo.Available, *actual.Rootfs.AvailableBytes)
		assert.Equal(imageFsInfo.Capacity, *actual.Rootfs.CapacityBytes)
		assert.Equal(*imageFsInfo.InodesFree, *actual.Rootfs.InodesFree)
		assert.Equal(*imageFsInfo.Inodes, *actual.Rootfs.Inodes)
	} else {
		assert.Nil(actual.Rootfs.AvailableBytes)
		assert.Nil(actual.Rootfs.CapacityBytes)
		assert.Nil(actual.Rootfs.InodesFree)
		assert.Nil(actual.Rootfs.Inodes)
	}
	assert.Equal(cs.WritableLayer.UsedBytes.Value, *actual.Rootfs.UsedBytes)
	assert.Equal(cs.WritableLayer.InodesUsed.Value, *actual.Rootfs.InodesUsed)
}

func checkCRILogsStats(assert *assert.Assertions, actual statsapi.ContainerStats, rootFsInfo *cadvisorapiv2.FsInfo) {
	assert.Equal(rootFsInfo.Timestamp, actual.Logs.Time.Time)
	assert.Equal(rootFsInfo.Available, *actual.Logs.AvailableBytes)
	assert.Equal(rootFsInfo.Capacity, *actual.Logs.CapacityBytes)
	assert.Equal(*rootFsInfo.InodesFree, *actual.Logs.InodesFree)
	assert.Equal(*rootFsInfo.Inodes, *actual.Logs.Inodes)
	assert.Nil(actual.Logs.UsedBytes)
	assert.Nil(actual.Logs.InodesUsed)
}
