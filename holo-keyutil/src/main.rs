// Holochain key utilities used during node provisioning.
//
// Subcommands:
//   sign            Sign data using a key in a running lair-keystore instance
//   extract-pubkey  Extract the raw ed25519 key from a Holochain AgentPubKey hash

use base64::Engine;
use clap::{Parser, Subcommand};
use holo_hash::AgentPubKey;
use lair_keystore_api::ipc_keystore::ipc_keystore_connect;
use lair_keystore_api::prelude::*;
use std::sync::{Arc, Mutex};

const B64: base64::engine::GeneralPurpose = base64::engine::general_purpose::URL_SAFE_NO_PAD;

#[derive(Parser)]
#[command(name = "holo-keyutil")]
struct Cli {
    #[command(subcommand)]
    command: Command,
}

#[derive(Subcommand)]
enum Command {
    /// Sign data using a key stored in a running lair-keystore instance.
    /// Prints the ed25519 signature as base64url to stdout.
    Sign {
        /// Lair connection URL (e.g. unix:///path/to/socket)
        lair_url: String,
        /// Lair passphrase
        passphrase: String,
        /// Agent public key as base64url (no padding)
        pubkey: String,
        /// Data to sign as base64url (no padding)
        data: String,
    },
    /// Extract the raw ed25519 public key from a Holochain AgentPubKey hash.
    /// Prints the 32-byte key as base64url (no padding) to stdout.
    ExtractPubkey {
        /// AgentPubKey holo_hash string (e.g. uhCAk...)
        agent_pub_key: String,
    },
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let cli = Cli::parse();
    match cli.command {
        Command::Sign {
            lair_url,
            passphrase,
            pubkey,
            data,
        } => {
            let connection_url: url::Url = lair_url.parse()?;
            let locked = sodoken::LockedArray::from(passphrase.as_bytes().to_vec());
            let passphrase = Arc::new(Mutex::new(locked));
            let client = ipc_keystore_connect(connection_url, passphrase).await?;

            let pub_key_bytes: [u8; 32] = B64
                .decode(&pubkey)?
                .try_into()
                .map_err(|_| anyhow::anyhow!("pubkey must be exactly 32 bytes"))?;
            let pub_key = BinDataSized(Arc::new(pub_key_bytes));

            let data_bytes = B64.decode(&data)?;
            let data: Arc<[u8]> = Arc::from(data_bytes.as_slice());

            let signature = client.sign_by_pub_key(pub_key, None, data).await?;
            println!("{}", B64.encode(signature.0.as_ref()));
        }
        Command::ExtractPubkey { agent_pub_key } => {
            let input = agent_pub_key.trim();

            // AgentPubKey holo_hash format: u<base64url([3-byte prefix][32-byte key][4-byte DHT loc])>
            let b64_part = input
                .strip_prefix('u')
                .ok_or_else(|| anyhow::anyhow!("missing multibase 'u' prefix"))?;

            let raw = B64.decode(b64_part)?;
            anyhow::ensure!(raw.len() == 39, "expected 39 decoded bytes, got {}", raw.len());

            // Validate using holo_hash — checks the type prefix bytes are correct for AgentPubKey
            let _ = AgentPubKey::from_raw_39(raw.clone());

            // Raw ed25519 key occupies bytes 3..35
            println!("{}", B64.encode(&raw[3..35]));
        }
    }
    Ok(())
}
