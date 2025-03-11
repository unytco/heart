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

# Create a new SSH key if not using existing
resource "digitalocean_ssh_key" "holochain" {
  count      = var.ssh_public_key != "" ? 1 : 0
  name       = "${var.node_name}-key"
  public_key = file(var.ssh_public_key)
}

resource "digitalocean_droplet" "holochain_node" {
  name     = var.node_name
  size     = var.droplet_size
  image    = "ubuntu-22-04-x64"
  region   = var.region
  ssh_keys = var.ssh_key_id != "" ? [var.ssh_key_id] : [digitalocean_ssh_key.holochain[0].id]

  connection {
    type        = "ssh"
    user        = "root"
    private_key = file(var.ssh_private_key)
    host        = self.ipv4_address
  }

  # Copy required files
  provisioner "file" {
    source      = "${path.module}/../scripts"
    destination = "/tmp"
  }

  provisioner "file" {
    source      = "${path.module}/../services"
    destination = "/tmp"
  }

  # Setup environment variables
  provisioner "remote-exec" {
    inline = [
      "echo 'export HOLOCHAIN_VERSION=${var.holochain_version}' >> /root/.profile",
      "echo 'export LAIR_PASSWORD=${var.lair_password}' >> /root/.profile",
      "echo 'export HOLOCHAIN_PASSWORD=${var.holochain_password}' >> /root/.profile",
      "chmod +x /tmp/scripts/*.sh",
      "/tmp/scripts/setup.sh",
      "/tmp/scripts/test.sh"
    ]
  }
}

output "droplet_ip" {
  value = digitalocean_droplet.holochain_node.ipv4_address
}

output "ssh_command" {
  value = "ssh root@${digitalocean_droplet.holochain_node.ipv4_address}"
} 