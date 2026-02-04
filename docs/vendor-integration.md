# Vendor Integration Guide

IZI SIPREC automatically detects and extracts vendor-specific metadata from various SBC and recording platforms. This document describes the supported vendors and their integration details.

## Supported Vendors

All 8 vendors have **complete data flow** from SIP headers through to CDR database records:

| Vendor | Detection Method | Key Fields | Status |
| --- | --- | --- | --- |
| **Oracle SBC** | User-Agent, X-Oracle-* headers | UCID, Conversation ID | Complete |
| **Cisco** | User-Agent, Session-ID header | Session ID, GUID | Complete |
| **Avaya** | User-Agent, X-Avaya-* headers | UCID, Conf ID, Station ID, Agent ID, VDN, Skill Group | Complete |
| **NICE** | User-Agent, X-NICE-* headers | Interaction ID, Session ID, Recording ID, Contact ID, Agent ID, Call ID | Complete |
| **Genesys** | User-Agent, X-Genesys-* headers | Interaction ID, Conversation ID, Session ID, Queue, Agent ID, Campaign ID | Complete |
| **Asterisk** | User-Agent, X-Asterisk-* headers | Unique ID, Linked ID, Channel ID, Account Code, Context | Complete |
| **FreeSWITCH** | User-Agent, X-FS-* headers | UUID, Core UUID, Channel Name, Profile Name, Account Code | Complete |
| **OpenSIPS** | User-Agent, X-OpenSIPS-* headers | Dialog ID, Transaction ID, Call-ID | Complete |
| **Generic** | Fallback | Standard SIPREC metadata | Complete |

## Data Flow Architecture

All vendor metadata flows through the complete pipeline:

```
SIP Headers → SIPMessage struct → ExtendedMetadata → SessionData (Redis) → CDR (Database)
                                                   ↓
                                            Analytics/Elasticsearch
```

Each vendor has:
- Dedicated fields in SIPMessage for structured access
- ExtendedMetadata storage for session persistence
- Redis SessionData fields for failover support
- CDR database columns for permanent storage
- Analytics integration for real-time processing

## Automatic Vendor Detection

The server automatically detects the vendor from:

1. **User-Agent header** - Primary detection method
2. **Custom SIP headers** - Fallback for vendor-specific headers
3. **SIPREC metadata** - XML extensions in the rs-metadata body

### Detection Priority

```
1. User-Agent string matching
2. Vendor-specific header presence
3. XML namespace detection in metadata
4. Default to "generic"
```

---

## Oracle SBC Integration

Oracle Session Border Controllers include Universal Call ID (UCID) for call correlation across systems.

### Detected Headers

| Header | Purpose | CDR Field |
| --- | --- | --- |
| `X-Oracle-UCID` | Universal Call ID | `oracle_ucid` |
| `X-OCSBC-UCID` | Alternative UCID | `oracle_ucid` |
| `X-Oracle-Conversation-ID` | Conversation correlation | `conversation_id` |
| `X-OCSBC-Conversation-ID` | Alternative conversation ID | `conversation_id` |
| `Session-ID` | RFC 7989 session identifier | - |
| `P-Charging-Vector` | IMS charging correlation | - |

### User-Agent Patterns

- `Oracle*`
- `Ocsbc*`
- `OCSBC*`
- `OESBC*`
- `Oesbc*`
- `Ocom*`

### Stored Metadata

| Metadata Key | Source |
| --- | --- |
| `sip_oracle_ucid` | X-Oracle-UCID or X-OCSBC-UCID |
| `sip_oracle_conversation_id` | X-Oracle-Conversation-ID |
| `sip_vendor_type` | "oracle" |

### Example Configuration

Oracle SBCs typically require no special configuration. Ensure SIPREC is enabled on the SBC:

```
session-router
  session-agent
    siprec-route-priority 1
```

---

## Cisco Integration

Cisco Unified Communications Manager and CUBE support SIPREC with session correlation.

### Detected Headers

| Header | Purpose | CDR Field |
| --- | --- | --- |
| `Session-ID` | Cisco Session-ID (RFC 7989) | `cisco_session_id` |
| `X-Cisco-Session-ID` | Alternative session identifier | `cisco_session_id` |
| `Cisco-GUID` | Global unique identifier | - |
| `X-Cisco-Call-ID` | Call identifier | - |
| `X-Cisco-Cluster-ID` | CUCM cluster | - |
| `X-Cisco-Device-Name` | Device name | - |

### User-Agent Patterns

- `Cisco*`
- `CUBE*`
- `Unified CM*`
- `CUCM*`

### Stored Metadata

| Metadata Key | Source |
| --- | --- |
| `sip_cisco_session_id` | Session-ID header |
| `sip_vendor_type` | "cisco" |

### Example CUBE Configuration

```
voice service voip
  media class 1
  siprec server ipv4:192.168.1.100 port 5060
```

---

## Avaya Integration

Avaya Aura, Communication Manager, and IP Office systems use UCID and various identifiers for call tracking.

### Detected Headers

| Header | Purpose | CDR Field |
| --- | --- | --- |
| `X-Avaya-UCID` | Universal Call ID | `avaya_ucid` |
| `X-UCID` | Alternative UCID | `avaya_ucid` |
| `User-to-User` | Contains UCID (RFC 7433) | `avaya_ucid` |
| `X-Avaya-Conf-ID` | Conference ID | `avaya_conf_id` |
| `X-Avaya-Station-ID` | Station identifier | `avaya_station_id` |
| `X-Avaya-Agent-ID` | Agent identifier | `avaya_agent_id` |
| `X-Avaya-VDN` | Vector Directory Number | `avaya_vdn` |
| `X-Avaya-Skill-Group` | Skill/Hunt group | `avaya_skill_group` |
| `X-Avaya-Skill` | Alternative skill header | `avaya_skill_group` |
| `X-Avaya-CM-Call-ID` | Communication Manager Call ID | - |
| `X-Avaya-SM-Session-ID` | Session Manager Session ID | - |
| `X-Avaya-Queue-Name` | ACD queue name | - |
| `X-Avaya-Workgroup` | Workgroup identifier | - |

### User-Agent Patterns

- `Avaya*`
- `IP Office*`
- `Aura*`
- `CM*` (Communication Manager)
- `Session Manager*`

### Stored Metadata

| Metadata Key | Source |
| --- | --- |
| `sip_avaya_ucid` | X-Avaya-UCID, X-UCID, or User-to-User |
| `sip_avaya_conf_id` | X-Avaya-Conf-ID |
| `sip_avaya_station_id` | X-Avaya-Station-ID |
| `sip_avaya_agent_id` | X-Avaya-Agent-ID |
| `sip_avaya_vdn` | X-Avaya-VDN |
| `sip_avaya_skill_group` | X-Avaya-Skill-Group or X-Avaya-Skill |
| `sip_vendor_type` | "avaya" |

### UCID Extraction

The server extracts UCID from multiple sources:

1. **X-Avaya-UCID header** (preferred)
2. **X-UCID header** (alternative)
3. **User-to-User header** (RFC 7433 format)

```
User-to-User: 00FAC9640001000100000001;encoding=hex
```

This is automatically parsed and stored as `avaya_ucid` in session metadata and CDR.

---

## NICE Integration

NICE systems (NICE Engage, NICE inContact, NICE CXone, NTR) are fully supported with comprehensive metadata extraction.

### Detected Headers

| Header | Purpose | CDR Field |
| --- | --- | --- |
| `X-NICE-Interaction-ID` | Interaction tracking | `nice_interaction_id` |
| `X-NICE-Session-ID` | Session identifier | `nice_session_id` |
| `X-NICE-Recording-ID` | Recording correlation | `nice_recording_id` |
| `X-NICE-Call-ID` | Call identifier | `nice_call_id` |
| `X-NICE-Contact-ID` | Contact tracking | `nice_contact_id` |
| `X-NICE-Agent-ID` | Agent identification | `nice_agent_id` |
| `X-NTR-Session-ID` | NTR session | `nice_session_id` |
| `X-NTR-Call-ID` | NTR call ID | `nice_call_id` |
| `X-inContact-Contact-ID` | inContact contact | `nice_contact_id` |
| `X-inContact-Agent-ID` | inContact agent | `nice_agent_id` |
| `X-CXone-Contact-ID` | CXone contact | `nice_contact_id` |
| `X-CXone-Agent-ID` | CXone agent | `nice_agent_id` |
| `X-Engage-Call-ID` | Engage call ID | `nice_call_id` |
| `X-Engage-Recording-ID` | Engage recording ID | `nice_recording_id` |
| `User-to-User` | May contain UCID | `ucid` |

### User-Agent Patterns

- `NICE*`
- `NTR*`
- `inContact*`
- `CXone*`
- `Engage Recording*`
- `Nexidia*`
- `Actimize*`

### Stored Metadata

| Metadata Key | Source |
| --- | --- |
| `sip_nice_interaction_id` | X-NICE-Interaction-ID or XML |
| `sip_nice_session_id` | X-NICE-Session-ID, X-NTR-Session-ID, or XML |
| `sip_nice_recording_id` | X-NICE-Recording-ID, X-Engage-Recording-ID, or XML |
| `sip_nice_call_id` | X-NICE-Call-ID, X-NTR-Call-ID, or XML |
| `sip_nice_contact_id` | X-NICE-Contact-ID, X-inContact-Contact-ID, X-CXone-Contact-ID |
| `sip_nice_agent_id` | X-NICE-Agent-ID, X-inContact-Agent-ID, X-CXone-Agent-ID |
| `sip_vendor_type` | "nice" |

### XML Extension Processing

NICE may embed additional metadata in the SIPREC XML body as extensions. The server automatically extracts:

- Interaction IDs
- Session IDs
- Recording IDs
- Contact IDs
- Agent IDs
- Custom NICE-specific fields

Example NICE metadata extension:

```xml
<recording xmlns="urn:ietf:params:xml:ns:recording:1">
  <session session_id="abc123">
    <nice:interactionId xmlns:nice="urn:nice:recording:1">INT-12345</nice:interactionId>
    <nice:agentId xmlns:nice="urn:nice:recording:1">AGENT-001</nice:agentId>
  </session>
</recording>
```

---

## Genesys Integration

Genesys Cloud, PureConnect, PureEngage, and GVP systems are fully supported.

### Detected Headers

| Header | Purpose | CDR Field |
| --- | --- | --- |
| `X-Genesys-Interaction-ID` | Interaction tracking | `genesys_interaction_id` |
| `X-Genesys-Conversation-ID` | Conversation correlation | `genesys_conversation_id` |
| `X-Genesys-Session-ID` | Session identifier | - |
| `X-Genesys-Queue-Name` | Queue name | `genesys_queue_name` |
| `X-Genesys-Agent-ID` | Agent identifier | `genesys_agent_id` |
| `X-Genesys-Campaign-ID` | Campaign tracking | `genesys_campaign_id` |
| `X-ININ-Interaction-ID` | ININ legacy interaction | `genesys_interaction_id` |
| `X-ININ-IC-UserID` | ININ user ID | - |
| `X-GVP-Session-ID` | GVP session | - |

### User-Agent Patterns

- `Genesys*`
- `PureConnect*`
- `PureCloud*`
- `PureEngage*`
- `Interaction*`
- `ININ*`
- `GVP*`

### Stored Metadata

| Metadata Key | Source |
| --- | --- |
| `sip_genesys_interaction_id` | X-Genesys-Interaction-ID or X-ININ-Interaction-ID |
| `sip_genesys_conversation_id` | X-Genesys-Conversation-ID |
| `sip_genesys_session_id` | X-Genesys-Session-ID |
| `sip_genesys_queue_name` | X-Genesys-Queue-Name |
| `sip_genesys_agent_id` | X-Genesys-Agent-ID |
| `sip_genesys_campaign_id` | X-Genesys-Campaign-ID |
| `sip_vendor_type` | "genesys" |

---

## Asterisk Integration

Asterisk and FreePBX recording sessions include channel and context information.

### Detected Headers

| Header | Purpose | CDR Field |
| --- | --- | --- |
| `X-Asterisk-Unique-ID` | Unique channel ID | `asterisk_unique_id` |
| `X-Asterisk-UniqueID` | Alternative unique ID | `asterisk_unique_id` |
| `X-Asterisk-LinkedID` | Linked channel ID | `asterisk_linked_id` |
| `X-Asterisk-Channel` | Channel name | `asterisk_channel_id` |
| `X-Asterisk-Channel-Name` | Channel name | `asterisk_channel_id` |
| `X-Asterisk-AccountCode` | Account code | `asterisk_account_code` |
| `X-Asterisk-CDR-AccountCode` | CDR account code | `asterisk_account_code` |
| `X-Asterisk-Context` | Dialplan context | `asterisk_context` |
| `X-Asterisk-Extension` | Extension | - |
| `X-Asterisk-Queue` | Queue name | - |
| `X-Asterisk-Agent` | Agent ID | - |
| `X-FPBX-DID` | FreePBX DID | - |
| `X-FPBX-RingGroup` | FreePBX ring group | - |

### User-Agent Patterns

- `Asterisk*`
- `FPBX*`
- `FreePBX*`
- `Sangoma*`

### Stored Metadata

| Metadata Key | Source |
| --- | --- |
| `sip_asterisk_unique_id` | X-Asterisk-Unique-ID |
| `sip_asterisk_linked_id` | X-Asterisk-LinkedID |
| `sip_asterisk_channel_id` | X-Asterisk-Channel |
| `sip_asterisk_account_code` | X-Asterisk-AccountCode |
| `sip_asterisk_context` | X-Asterisk-Context |
| `sip_vendor_type` | "asterisk" |

---

## FreeSWITCH Integration

FreeSWITCH recording sessions include UUID correlation and Sofia profile information.

### Detected Headers

| Header | Purpose | CDR Field |
| --- | --- | --- |
| `X-FS-UUID` | FreeSWITCH call UUID | `freeswitch_uuid` |
| `X-FreeSWITCH-UUID` | Alternative UUID | `freeswitch_uuid` |
| `X-FS-Core-UUID` | Core instance UUID | `freeswitch_core_uuid` |
| `X-FreeSWITCH-Core-UUID` | Alternative core UUID | `freeswitch_core_uuid` |
| `X-FS-Channel-Name` | Channel name | `freeswitch_channel_name` |
| `X-FS-Profile-Name` | Sofia profile | `freeswitch_profile_name` |
| `X-FS-Sofia-Profile` | Sofia profile | `freeswitch_profile_name` |
| `X-FS-AccountCode` | Account code | `freeswitch_account_code` |
| `X-FS-Billing-Code` | Billing code | `freeswitch_account_code` |
| `X-FS-Bridge-UUID` | Bridge UUID | - |
| `X-FS-Other-Leg-UUID` | Other leg UUID | - |
| `X-FS-CC-Queue` | Call center queue | - |
| `X-FS-CC-Agent` | Call center agent | - |

### User-Agent Patterns

- `FreeSWITCH*`
- `FreeSwitch*`
- `mod_sofia*`

### Stored Metadata

| Metadata Key | Source |
| --- | --- |
| `sip_freeswitch_uuid` | X-FS-UUID or X-FreeSWITCH-UUID |
| `sip_freeswitch_core_uuid` | X-FS-Core-UUID |
| `sip_freeswitch_channel_name` | X-FS-Channel-Name |
| `sip_freeswitch_profile_name` | X-FS-Profile-Name or X-FS-Sofia-Profile |
| `sip_freeswitch_account_code` | X-FS-AccountCode or X-FS-Billing-Code |
| `sip_vendor_type` | "freeswitch" |

---

## OpenSIPS Integration

OpenSIPS and Kamailio recording sessions include dialog and transaction information.

### Detected Headers

| Header | Purpose | CDR Field |
| --- | --- | --- |
| `X-OpenSIPS-Dialog-ID` | Dialog identifier | `opensips_dialog_id` |
| `X-OpenSIPS-Transaction-ID` | Transaction ID | `opensips_transaction_id` |
| `X-OpenSIPS-Call-ID` | Call-ID correlation | `opensips_call_id` |
| `X-OpenSIPS-DLG-CallID` | Dialog Call-ID | `opensips_call_id` |
| `X-Kamailio-Dialog-ID` | Kamailio dialog ID | `opensips_dialog_id` |
| `X-Kamailio-Transaction-ID` | Kamailio transaction ID | `opensips_transaction_id` |
| `X-OpenSIPS-Dispatcher-Dst` | Dispatcher destination | - |
| `X-OpenSIPS-Dispatcher-SetID` | Dispatcher set | - |
| `X-OpenSIPS-RTPProxy-ID` | RTPProxy ID | - |
| `X-OpenSIPS-RTPEngine-ID` | RTPEngine ID | - |

### User-Agent Patterns

- `OpenSIPS*`
- `opensips*`
- `Kamailio*`
- `kamailio*`

### Stored Metadata

| Metadata Key | Source |
| --- | --- |
| `sip_opensips_dialog_id` | X-OpenSIPS-Dialog-ID or X-Kamailio-Dialog-ID |
| `sip_opensips_transaction_id` | X-OpenSIPS-Transaction-ID or X-Kamailio-Transaction-ID |
| `sip_opensips_call_id` | X-OpenSIPS-Call-ID or X-OpenSIPS-DLG-CallID |
| `sip_vendor_type` | "opensips" |

---

## Universal Call ID (UCID) Support

The server extracts UCID from multiple sources across all vendors:

### User-to-User Header (RFC 7433)

The User-to-User header is parsed for UCID data across all vendors:

```
User-to-User: 00FAC9640001000100000001;encoding=hex
```

Supported header variations:
- `User-to-User`
- `UUI`
- `X-User-to-User`

### Vendor-Specific UCID Headers

| Vendor | Header | CDR Field |
| --- | --- | --- |
| Oracle | `X-Oracle-UCID` | `oracle_ucid` |
| Avaya | `X-Avaya-UCID` | `avaya_ucid` |
| Generic | `X-UCID` | `ucid` |

---

## XML Extension Support

The server captures arbitrary XML extensions from SIPREC metadata. Extensions can appear in:

- Session elements
- Recording session elements
- Participant elements
- Stream elements
- Group elements

### Extension Storage

Extensions are stored in `session.ExtendedMetadata` with normalized keys:

```
ext_<context>_<namespace>_<element>
```

Example:
```
ext_session_0_nice_interactionid = "INT-12345"
ext_participant_0_custom_agentcode = "A001"
```

### Vendor-Specific Extension Processing

The server automatically extracts known vendor fields from extensions:

| Vendor | Detected Extensions |
| --- | --- |
| NICE | interactionId, sessionId, recordingId, contactId, agentId, callId, ucid |
| Oracle | ucid, conversationId |
| Cisco | sessionId, guid |
| Genesys | conversationId, interactionId |
| Avaya | ucid, confId, stationId |

---

## Analytics Integration

All vendor metadata flows to the analytics pipeline and is available in:

### Elasticsearch Documents

```json
{
  "call_id": "call-123",
  "vendor_type": "avaya",
  "avaya_ucid": "00FAC9640001000100000001",
  "avaya_station_id": "1001",
  "avaya_agent_id": "AGENT-001",
  "avaya_vdn": "5000"
}
```

### CDR Database Records

All vendor-specific fields are stored in dedicated CDR columns:

```sql
SELECT
  session_id,
  vendor_type,
  avaya_ucid,
  avaya_agent_id,
  nice_interaction_id,
  genesys_conversation_id,
  cisco_session_id
FROM cdr
WHERE vendor_type = 'avaya';
```

### WebSocket Analytics Stream

```json
{
  "event_type": "transcript",
  "call_id": "call-123",
  "metadata": {
    "vendor_type": "avaya",
    "avaya_ucid": "00FAC9640001000100000001"
  }
}
```

### Redis Session Store

Vendor metadata is preserved in Redis for failover scenarios:

```json
{
  "session_id": "sess-123",
  "vendor_type": "avaya",
  "avaya_ucid": "00FAC9640001000100000001",
  "avaya_conf_id": "CONF-123",
  "avaya_station_id": "1001",
  "avaya_agent_id": "AGENT-001",
  "avaya_vdn": "5000",
  "avaya_skill_group": "Sales",
  "extended_metadata": {
    "sip_avaya_ucid": "00FAC9640001000100000001"
  }
}
```

---

## Troubleshooting

### Vendor Not Detected

If vendor detection fails:

1. Check User-Agent header in SIP INVITE
2. Verify vendor-specific headers are present
3. Enable debug logging to see header extraction:

```bash
LOG_LEVEL=debug
```

Look for logs like:
```
Detected SIP vendor: avaya
Extracted Avaya headers: ucid=00FAC9640001000100000001, station_id=1001
```

### Missing Metadata

If expected metadata is missing:

1. Verify the SRC is sending the headers
2. Check if headers match expected patterns (case-insensitive)
3. For XML extensions, ensure proper namespace handling
4. Check the ExtendedMetadata map in session data

### UCID Not Extracted

The User-to-User header must be present and properly formatted:

```
User-to-User: <hex-encoded-data>;encoding=hex
```

Or as plain text:
```
User-to-User: UCID-12345
```

### CDR Fields Empty

If CDR vendor fields are empty:

1. Verify the SIPMessage struct is being populated
2. Check that ExtendedMetadata has the `sip_<vendor>_*` keys
3. Ensure the CDR update block in custom_server.go is being executed
4. Check CDR service logs for update errors

---

## Adding Custom Vendor Support

To add support for a new vendor:

1. Add User-Agent patterns to `detectVendor()` in `custom_server.go`
2. Add header extraction function (e.g., `extractVendorHeaders()`)
3. Add dedicated fields to SIPMessage struct
4. Add ExtendedMetadata storage in `storeVendorMetadataInSession()`
5. Add fields to SessionData in `pkg/session/redis_store.go`
6. Add fields to CDR model in `pkg/database/models.go`
7. Add fields to CDRUpdate struct in `pkg/cdr/service.go`
8. Update CDR service `UpdateSession()` method
9. Add CDR update block in custom_server.go

Contact IZI Technologies for custom vendor integration support.
