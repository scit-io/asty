package asty

import (
	"asty/internal/platform/asty/core/config"
	"asty/internal/platform/asty/core/types"
	"asty/internal/platform/asty/features/autoscaling"
	autometrics "asty/internal/platform/asty/features/autoscaling/metrics"
	"asty/internal/platform/asty/features/clustering/controller"
	"asty/internal/platform/asty/features/clustering/discovery"
	"asty/internal/platform/asty/features/clustering/leader"
	"asty/internal/platform/asty/features/clustering/state"
	"asty/internal/platform/asty/features/deployment"
	"asty/internal/platform/asty/features/deployment/artifacts"
	"asty/internal/platform/asty/features/draining"
	"asty/internal/platform/asty/features/execution/health"
	"asty/internal/platform/asty/features/execution/process"
	"asty/internal/platform/asty/features/observability/logs"
	"asty/internal/platform/asty/features/observability/metrics"
	"asty/internal/platform/asty/features/scheduling"
	"asty/internal/platform/asty/features/scheduling/proximity"
)

// --- core/config ---

type Config = config.Config

// --- core/types ---

type NodeInfo = types.NodeInfo
type ServiceAllocation = types.ServiceAllocation
type ServiceCooldown = types.ServiceCooldown
type ServiceType = types.ServiceType
type ServiceDefinition = types.ServiceDefinition
type Artifact = types.Artifact
type Resources = types.Resources
type Health = types.Health
type Logs = types.Logs
type Update = types.Update
type Restart = types.Restart
type ClusterEvent = types.ClusterEvent
type Command = types.Command
type StartServiceCommand = types.StartServiceCommand
type StopServiceCommand = types.StopServiceCommand
type GetLogsCommand = types.GetLogsCommand
type LogsResponse = types.LogsResponse
type CommandResponse = types.CommandResponse

const (
	ServiceTypeSystem  = types.ServiceTypeSystem
	ServiceTypeService = types.ServiceTypeService
)

var (
	MarshalStartCommand   = types.MarshalStartCommand
	MarshalStopCommand    = types.MarshalStopCommand
	MarshalGetLogsCommand = types.MarshalGetLogsCommand
	UnmarshalCommand      = types.UnmarshalCommand
	ParseStartCommand     = types.ParseStartCommand
	ParseStopCommand      = types.ParseStopCommand
	ParseGetLogsCommand   = types.ParseGetLogsCommand
	MarshalResponse       = types.MarshalResponse
	MarshalLogsResponse   = types.MarshalLogsResponse
)

var newEvent = types.NewEvent

// --- features/clustering/state ---

type ClusterState = state.ClusterState

var NewClusterState = state.New

// --- features/clustering/leader ---

type LeaderElection = leader.Election
type LeaderInfo = leader.Info

var NewLeaderElection = leader.NewElection

// --- features/clustering/discovery ---

type NodeDiscovery = discovery.NodeDiscovery

var NewNodeDiscovery = discovery.New

// --- features/clustering/controller ---

type ServiceController = controller.ServiceController
type CommandDispatcher = controller.CommandDispatcher

var NewServiceController = controller.NewServiceController

type Workqueue = controller.Workqueue

var NewWorkqueue = controller.NewWorkqueue

// --- features/scheduling ---

type Scheduler = scheduling.Scheduler
type Placement = scheduling.Placement

var NewScheduler = scheduling.NewScheduler

// --- features/scheduling/proximity ---

type ProximityMatrix = proximity.Matrix

var NewProximityMatrix = proximity.NewMatrix

// --- features/autoscaling ---

type Autoscaler = autoscaling.Autoscaler
type ScalingDecision = autoscaling.ScalingDecision

var NewAutoscaler = autoscaling.NewAutoscaler

// --- features/autoscaling/metrics ---

type MetricsStore = autometrics.Store
type MetricPoint = autometrics.MetricPoint
type ScalingEvent = autometrics.ScalingEvent

var NewMetricsStore = autometrics.NewStore

// --- features/deployment ---

type Deployer = deployment.Deployer
type DeploymentRecord = deployment.DeploymentRecord
type DeploymentPlan = deployment.DeploymentPlan
type UpdateStrategy = deployment.UpdateStrategy
type DeploymentStatus = deployment.DeploymentStatus
type ServiceLoader = deployment.ServiceLoader

var (
	NewServiceLoader      = deployment.NewServiceLoader
	LoadServiceDefinition = deployment.LoadServiceDefinition
)

// --- features/deployment/artifacts ---

type ArtifactDownloader = artifacts.Downloader

var NewArtifactDownloader = artifacts.NewDownloader

// --- features/draining ---

type DrainStatus = draining.DrainStatus
type DrainManager = draining.DrainManager
type DrainDeps = draining.DrainDeps

var NewDrainManager = draining.NewDrainManager

// --- features/execution/process ---

type Process = process.Process
type ProcessStatus = process.Status

const (
	ProcessStatusStarting = process.StatusStarting
	ProcessStatusRunning  = process.StatusRunning
	ProcessStatusStopping = process.StatusStopping
	ProcessStatusStopped  = process.StatusStopped
	ProcessStatusFailed   = process.StatusFailed
)

var NewProcess = process.New

// --- features/execution/health ---

type HealthChecker = health.Checker
type HealthCheck = health.Check

var NewHealthChecker = health.NewChecker

// --- features/observability/metrics ---

type MetricsCollector = metrics.Collector
type ProcessMetrics = metrics.ProcessMetrics

var NewMetricsCollector = metrics.NewCollector

// --- features/observability/logs ---

type LogLine = logs.LogLine
type LogBuffer = logs.Buffer
type NATSWriter = logs.NATSWriter

var (
	NewLogBuffer  = logs.NewBuffer
	NewNATSWriter = logs.NewNATSWriter
)
