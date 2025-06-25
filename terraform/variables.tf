variable "do_token" {
  description = "DigitalOcean API token"
  type        = string
}

variable "ssh_key_id" {
  description = "ID or fingerprint of SSH key in DigitalOcean"
  type        = string
  default     = ""
}

variable "ssh_public_key" {
  description = "Path to SSH public key file"
  type        = string
  default     = "~/.ssh/id_rsa.pub"
}

variable "ssh_private_key" {
  description = "Path to SSH private key file"
  type        = string
  default     = "~/.ssh/id_rsa"
}

variable "node_name" {
  description = "Name of the Holochain node"
  type        = string
  default     = "holochain-node"
}

variable "droplet_size" {
  description = "Size of the DigitalOcean droplet"
  type        = string
  default     = "s-4vcpu-8gb"
}

variable "region" {
  description = "DigitalOcean region"
  type        = string
  default     = "nyc3"
}

variable "holochain_version" {
  description = "Version of Holochain to install"
  type        = string
  default     = "0.5.2"
}

variable "lair_version" {
  description = "Version of Lair to install"
  type        = string
  default     = "0.6.1"
}

variable "lair_password" {
  description = "Password for Lair keystore"
  type        = string
  default     = "secure-password"
}

variable "holochain_password" {
  description = "Password for Holochain conductor"
  type        = string
  default     = "secure-password"
} 