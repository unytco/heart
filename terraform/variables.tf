variable "do_token" {
  description = "DigitalOcean API token"
  type        = string
}

variable "ssh_key_id" {
  description = "ID of SSH key in DigitalOcean"
  type        = string
}

variable "node_name" {
  description = "Name of the Holochain node"
  type        = string
  default     = "holochain-node"
}

variable "droplet_size" {
  description = "Size of the DigitalOcean droplet"
  type        = string
  default     = "s-2vcpu-4gb"
}

variable "region" {
  description = "DigitalOcean region"
  type        = string
  default     = "nyc3"
}

variable "holochain_version" {
  description = "Version of Holochain to install"
  type        = string
  default     = "0.2.3"
} 