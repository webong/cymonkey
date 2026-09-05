// Package cymonkey is the public façade for Cymonkey's internal core.
// New integrations inside this repository should import the core directly.
package cymonkey

import core "cymonkey/internal/cymonkeycore"

const ProtocolVersion = core.ProtocolVersion

type (
	Domain           = core.Domain
	ModuleKind       = core.ModuleKind
	Caller           = core.Caller
	Attachment       = core.Attachment
	Endpoint         = core.Endpoint
	Target           = core.Target
	Policy           = core.Policy
	AttachOptions    = core.AttachOptions
	RuntimeBinding   = core.RuntimeBinding
	DriverDescriptor = core.DriverDescriptor
	ModuleDescriptor = core.ModuleDescriptor
	Module           = core.Module
	ModuleFunc       = core.ModuleFunc
	Registry         = core.Registry
	Hello            = core.Hello
	Capability       = core.Capability
)

const (
	DomainViewer  = core.DomainViewer
	DomainRender  = core.DomainRender
	DomainPlayer  = core.DomainPlayer
	RuntimeModule = core.RuntimeModule
	DriverModule  = core.DriverModule
)

var (
	ValidDomain               = core.ValidDomain
	ValidIdentifier           = core.ValidIdentifier
	ValidateModuleDescriptor  = core.ValidateModuleDescriptor
	NewRegistry               = core.NewRegistry
	NewComposite              = core.NewComposite
	ValidateConformance       = core.ValidateConformance
	ValidateModuleConformance = core.ValidateModuleConformance
)
