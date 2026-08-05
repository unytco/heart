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
        #[arg(allow_hyphen_values = true)]
        pubkey: String,
        /// Data to sign as base64url (no padding)
        #[arg(allow_hyphen_values = true)]
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
            println!("{}", extract_pubkey(&agent_pub_key)?);
        }
    }
    Ok(())
}

/// Extract the raw ed25519 key from an AgentPubKey holo_hash string, as base64url.
///
/// This is what registration sends to the auth server, so every rejection here is
/// a node that never registers - worth an error naming the reason rather than the
/// panic `AgentPubKey::from_raw_39` would raise on a hash of the wrong type.
fn extract_pubkey(agent_pub_key: &str) -> anyhow::Result<String> {
    // AgentPubKey holo_hash format: u<base64url([3-byte prefix][32-byte key][4-byte DHT loc])>
    let b64_part = agent_pub_key
        .trim()
        .strip_prefix('u')
        .ok_or_else(|| anyhow::anyhow!("missing multibase 'u' prefix"))?;

    let raw = B64.decode(b64_part)?;
    anyhow::ensure!(
        raw.len() == 39,
        "expected 39 decoded bytes, got {}",
        raw.len()
    );

    // Rejects a well-formed hash of some other type - a DnaHash has the same
    // shape, and its bytes 3..35 are a DNA hash, not anybody's public key.
    AgentPubKey::try_from_raw_39(raw.clone())
        .map_err(|e| anyhow::anyhow!("not an AgentPubKey: {e}"))?;

    // Raw ed25519 key occupies bytes 3..35
    Ok(B64.encode(&raw[3..35]))
}

#[cfg(test)]
mod tests {
    use super::*;

    // A real AgentPubKey and the raw key it carries. Golden rather than
    // round-tripped: the point is that the bytes reaching the auth server do not
    // move when the holo_hash dependency does, and a value derived through the
    // same library would shift with it silently.
    const AGENT: &str = "uhCAkN5IokFxdryZWUzR6Nb89wjVsiENaXp8uGsKbGJpT1SKxPzEm";
    const RAW: &str = "N5IokFxdryZWUzR6Nb89wjVsiENaXp8uGsKbGJpT1SI";

    #[test]
    fn extracts_the_raw_key() {
        assert_eq!(extract_pubkey(AGENT).unwrap(), RAW);
    }

    #[test]
    fn tolerates_surrounding_whitespace() {
        // The caller is a shell pipeline, so a trailing newline is the norm.
        assert_eq!(extract_pubkey(&format!(" {AGENT}\n")).unwrap(), RAW);
    }

    #[test]
    fn rejects_malformed_input() {
        for (name, input) in [
            (
                "no multibase prefix",
                AGENT.trim_start_matches('u').to_string(),
            ),
            ("not base64url", "u!!!!".to_string()),
            ("too short", format!("u{}", B64.encode([0u8; 38]))),
            ("too long", format!("u{}", B64.encode([0u8; 40]))),
        ] {
            assert!(
                extract_pubkey(&input).is_err(),
                "{name}: extract_pubkey({input:?}) should not have succeeded"
            );
        }
    }

    #[test]
    fn rejects_a_hash_of_another_type() {
        // Same 39-byte shape with a DnaHash prefix (uhC0k), so length and base64
        // both pass and only holo_hash's prefix check stands between it and 32
        // bytes of DNA hash sent to the auth server as somebody's public key.
        // Asserted on the message, since every other case here is also is_err().
        let err = extract_pubkey(&AGENT.replacen("uhCAk", "uhC0k", 1))
            .expect_err("a DnaHash is not an AgentPubKey")
            .to_string();
        assert!(
            err.contains("not an AgentPubKey") && err.contains("unknown prefix"),
            "error should name the prefix check, got {err:?}"
        );
    }
}
