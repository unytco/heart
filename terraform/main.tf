terraform {
  required_providers {
    digitalocean = {
      source  = "digitalocean/digitalocean"
      version = "~> 2.0"
    }
  }
}

provider "digitalocean" {
  token = var.do_token
}

resource "digitalocean_droplet" "holochain_node" {
  name     = var.node_name
  size     = var.droplet_size
  image    = "nixos-23.05-x64"
  region   = var.region
  ssh_keys = [var.ssh_key_id]

  provisioner "file" {
    source      = "${path.module}/../scripts/setup.sh"
    destination = "/tmp/setup.sh"
  }

  provisioner "file" {
    source      = "${path.module}/../scripts/test.sh"
    destination = "/tmp/test.sh"
  }

  provisioner "remote-exec" {
    inline = [
      "chmod +x /tmp/setup.sh",
      "chmod +x /tmp/test.sh",
      "HOLOCHAIN_VERSION=${var.holochain_version} /tmp/setup.sh",
      "/tmp/test.sh"
    ]
  }
}

output "ip_address" {
  value = digitalocean_droplet.holochain_node.ipv4_address
} 