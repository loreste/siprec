# Vendor Integration Guide

IZI SIPREC automatically detects and extracts vendor-specific metadata from various SBC and recording platforms. This document describes the supported vendors and their integration details.

## Supported Vendors

All 13 vendors have **complete data flow** from SIP headers through to CDR database records:

| Vendor | Detection Method | Key Fields | Status |
| --- | --- | --- | --- |
| **Oracle SBC** | User-Agent, X-Oracle-* headers | UCID, Conversation ID | Complete |
| **Cisco** | User-Agent, Session-ID header | Session ID, GUID | Complete |
| **Avaya** | User-Agent, X-Avaya-* headers | UCID, Conf ID, Station ID, Agent ID, VDN, Skill Group | Complete |
| **AudioCodes** | User-Agent (Device/, Mediant), X-AC-* headers | Session ID, Call ID, X-AC-Action | Complete |
| **Ribbon** | User-Agent (Ribbon, Sonus, GENBAND), X-Ribbon-* headers | Session ID, Call ID, Gateway ID | Complete |
| **Sansay** | User-Agent (Sansay, VSXi), X-Sansay-* headers | Session ID, Call ID, Trunk ID | Complete |
| **Huawei** | User-Agent (Huawei, eSpace), X-Huawei-* headers | Session ID, Call ID, Trunk ID, ICID | Complete |
| **Microsoft** | User-Agent (Teams, Skype, Lync), ms-conversation-id | Conversation ID, Call ID, Correlation ID | Complete |
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

Oracle Session Border Controllers include Universal Call ID (UCID) for call correlation across systems. IZI SIPREC supports both SIP header extraction and XML metadata parsing for Oracle-specific fields.

### Detected Headers

| Header | Purpose | CDR Field |
| --- | --- | --- |
| `X-Oracle-UCID` | Universal Call ID | `oracle_ucid` |
| `X-OCSBC-UCID` | Alternative UCID | `oracle_ucid` |
| `P-OCSBC-UCID` | P-header UCID variant | `oracle_ucid` |
| `X-Oracle-Conversation-ID` | Conversation correlation | `conversation_id` |
| `X-OCSBC-Conversation-ID` | Alternative conversation ID | `conversation_id` |
| `Session-ID` | RFC 7989 session identifier | - |
| `P-Charging-Vector` | IMS charging correlation (ICID) | - |

### XML Extension Data (SIPREC Metadata Body)

Oracle SBCs embed vendor-specific metadata in the SIPREC XML body using the ACME Packet namespace. IZI SIPREC automatically extracts these fields:

| XML Element | Namespace | Purpose | Metadata Key |
| --- | --- | --- | --- |
| `<apkt:ucid>` | `http://acmepacket.com/siprec/extensiondata` | Universal Call ID (hex encoded) | `sip_oracle_ucid` |
| `<apkt:callerOrig>` | `http://acmepacket.com/siprec/extensiondata` | Whether call is caller-originated | `oracle_caller_orig` |
| `<apkt:callingParty>` | `http://acmepacket.com/siprec/extensiondata` | Identifies calling party participant | `oracle_calling_party_id` |

#### Example Oracle SIPREC XML Metadata

```xml
<recording xmlns='urn:ietf:params:xml:ns:recording'>
  <datamode>complete</datamode>
  <session id="KaN/G7StRcxaeBcmQo4f5w==">
    <associate-time>2026-02-13T11:39:08</associate-time>
    <extensiondata xmlns:apkt="http://acmepacket.com/siprec/extensiondata">
      <apkt:ucid>00FA080018803B69810C6D;encoding=hex</apkt:ucid>
      <apkt:callerOrig>true</apkt:callerOrig>
    </extensiondata>
  </session>
  <participant id="Xe/H7yetSK5+rgO5w1ZqMg==" session="KaN/G7StRcxaeBcmQo4f5w==">
    <nameID aor="sip:4078729209@10.40.0.15">
      <name>"WIRELESS CALLER"</name>
    </nameID>
    <send>MQk402CcRc1fZeyhuGCFFg==</send>
    <extensiondata xmlns:apkt="http://acmepacket.com/siprec/extensiondata">
      <apkt:callingParty>true</apkt:callingParty>
    </extensiondata>
  </participant>
  <participant id="OPqY7Pz+S1RdYbmtYJuQpg==" session="KaN/G7StRcxaeBcmQo4f5w==">
    <nameID aor="sip:4079395277@10.40.8.2">
      <name>4079395277</name>
    </nameID>
    <send>zfbJ3As/TeNcB0931nNCeA==</send>
    <extensiondata xmlns:apkt="http://acmepacket.com/siprec/extensiondata">
      <apkt:callingParty>false</apkt:callingParty>
    </extensiondata>
  </participant>
  <stream id="MQk402CcRc1fZeyhuGCFFg==" session="KaN/G7StRcxaeBcmQo4f5w==">
    <label>771757897</label>
    <mode>separate</mode>
  </stream>
</recording>
```

### User-Agent Patterns

- `Oracle*`
- `Ocsbc*`
- `OCSBC*`
- `OESBC*`
- `Oesbc*`
- `Ocom*`
- `ACME Packet*`

### Stored Metadata

| Metadata Key | Source | Description |
| --- | --- | --- |
| `sip_oracle_ucid` | X-Oracle-UCID header OR `<apkt:ucid>` XML element | Universal Call ID for correlation |
| `sip_oracle_conversation_id` | X-Oracle-Conversation-ID header | Conversation tracking |
| `oracle_caller_orig` | `<apkt:callerOrig>` XML element | "true" if caller-originated |
| `oracle_calling_party_id` | `<apkt:callingParty>` XML element | Participant ID of calling party |
| `sip_vendor_type` | Auto-detected | "oracle" |

### UCID Extraction Priority

The server extracts Oracle UCID from multiple sources in this priority order:

1. **XML Metadata** - `<apkt:ucid>` element in session extensiondata (most reliable)
2. **SIP Headers** - `X-Oracle-UCID`, `X-OCSBC-UCID`, `P-OCSBC-UCID`
3. **P-Charging-Vector** - ICID field as fallback

The UCID value is automatically cleaned (`;encoding=hex` suffix removed) before storage.

### Example Configuration

Oracle SBCs typically require no special configuration. Ensure SIPREC is enabled on the SBC:

```
session-router
  session-agent
    siprec-route-priority 1
```

For SBC 9.x and later:
```
media-manager
  realm-config
    siprec-enabled enabled
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

## AudioCodes Integration

AudioCodes Mediant SBC series support SIPREC with on-demand recording control via the X-AC-Action header.

### Detected Headers

| Header | Purpose | CDR Field |
| --- | --- | --- |
| `X-AC-Action` | On-demand recording control (start-siprec, pause-siprec, etc.) | - |
| `X-AC-Session-ID` | AudioCodes session identifier | `audiocodes_session_id` |
| `X-AudioCodes-Session-ID` | Alternative session ID | `audiocodes_session_id` |
| `X-AC-Call-ID` | AudioCodes call identifier | `audiocodes_call_id` |
| `X-AC-Recording-Action` | Recording action type | - |
| `X-AC-Recording-Server` | Recording server destination | - |
| `X-AC-Recording-IP-Group` | Recording IP group | - |
| `X-AC-Source-IP-Group` | Source IP group | - |
| `X-AC-Dest-IP-Group` | Destination IP group | - |
| `X-AC-Avaya-UCID` | Avaya UCID (interworking mode) | `ucid` |

### User-Agent Patterns

- `AudioCodes*`
- `Mediant*`
- `Device /*` (e.g., "Device /7.40A.600.231")

### Stored Metadata

| Metadata Key | Source |
| --- | --- |
| `sip_audiocodes_session_id` | X-AC-Session-ID or X-AudioCodes-Session-ID |
| `sip_audiocodes_call_id` | X-AC-Call-ID |
| `sip_audiocodes_ac_action` | X-AC-Action |
| `sip_vendor_type` | "audiocodes" |

### X-AC-Action Header

The X-AC-Action header controls on-demand SIPREC sessions:

```
X-AC-Action: start-siprec;recording-ip-group=SRS_Group;recorded-side=peer
```

Supported actions:
- `start-siprec` - Start recording
- `pause-siprec` - Pause recording
- `resume-siprec` - Resume recording
- `stop-siprec` - Stop recording

### Avaya Interworking

AudioCodes can extract Avaya UCID from User-to-User headers and include it in its own XML metadata as `<ac:AvayaUCID>`. This is automatically extracted and stored.

---

## Ribbon Integration

Ribbon SBC (formerly Sonus/GENBAND) supports SIPREC with configurable metadata profiles.

### Detected Headers

| Header | Purpose | CDR Field |
| --- | --- | --- |
| `X-Ribbon-Session-ID` | Ribbon session identifier | `ribbon_session_id` |
| `X-Ribbon-Call-ID` | Ribbon call identifier | `ribbon_call_id` |
| `X-Ribbon-GW-ID` | Gateway identifier | `ribbon_gw_id` |
| `X-Ribbon-Trunk-Group` | Trunk group name | - |
| `X-Ribbon-Recording-ID` | Recording identifier | - |
| `X-Ribbon-SIPREC-Session` | SIPREC session ID | - |
| `X-Ribbon-Route` | Route information | - |
| `X-Ribbon-Zone` | Zone identifier | - |
| `X-Ribbon-Carrier-ID` | Carrier identifier | - |
| `X-Ribbon-Billing-ID` | Billing identifier | - |
| `X-Sonus-Session-ID` | Legacy Sonus session ID | `ribbon_session_id` |
| `X-Sonus-Call-ID` | Legacy Sonus call ID | `ribbon_call_id` |
| `X-GENBAND-Session-ID` | Legacy GENBAND session ID | `ribbon_session_id` |

### User-Agent Patterns

- `Ribbon*`
- `Sonus*`
- `GENBAND*`
- `SBC Edge*`
- `SBC Core*`
- `SWe Lite*`

### Stored Metadata

| Metadata Key | Source |
| --- | --- |
| `sip_ribbon_session_id` | X-Ribbon-Session-ID, X-Sonus-Session-ID, or X-GENBAND-Session-ID |
| `sip_ribbon_call_id` | X-Ribbon-Call-ID or X-Sonus-Call-ID |
| `sip_ribbon_gw_id` | X-Ribbon-GW-ID or X-Sonus-GW-ID |
| `sip_vendor_type` | "ribbon" |

### SIPREC Metadata Profiles

Ribbon SBC supports customizable SIPREC metadata profiles (sipRecMetaDataProfile) that map SIP headers to XML metadata elements. The server automatically extracts both standard SIPREC metadata and any custom Ribbon extensions.

---

## Sansay Integration

Sansay VSXi SBC supports SIPREC with trunk and routing information.

### Detected Headers

| Header | Purpose | CDR Field |
| --- | --- | --- |
| `X-Sansay-Session-ID` | Sansay session identifier | `sansay_session_id` |
| `X-VSXi-Session-ID` | VSXi session identifier | `sansay_session_id` |
| `X-Sansay-Call-ID` | Sansay call identifier | `sansay_call_id` |
| `X-VSXi-Call-ID` | VSXi call identifier | `sansay_call_id` |
| `X-Sansay-Trunk-ID` | Trunk identifier | `sansay_trunk_id` |
| `X-Sansay-Trunk-Group` | Trunk group name | - |
| `X-Sansay-Ingress-Trunk` | Ingress trunk ID | `sansay_trunk_id` |
| `X-Sansay-Egress-Trunk` | Egress trunk ID | - |
| `X-Sansay-Route-ID` | Route identifier | - |
| `X-Sansay-LCR-Route` | LCR route information | - |
| `X-Sansay-Billing-ID` | Billing identifier | - |
| `X-Sansay-Account-Code` | Account code | - |
| `X-Sansay-Carrier-ID` | Carrier identifier | - |
| `X-Sansay-ANI` | ANI information | - |
| `X-Sansay-DNIS` | DNIS information | - |

### User-Agent Patterns

- `Sansay*`
- `VSXi*`
- `VSX *`

### Stored Metadata

| Metadata Key | Source |
| --- | --- |
| `sip_sansay_session_id` | X-Sansay-Session-ID or X-VSXi-Session-ID |
| `sip_sansay_call_id` | X-Sansay-Call-ID or X-VSXi-Call-ID |
| `sip_sansay_trunk_id` | X-Sansay-Trunk-ID or X-Sansay-Ingress-Trunk |
| `sip_vendor_type` | "sansay" |

### Trunk and Routing Information

Sansay VSXi provides detailed trunk and routing information that can be useful for:
- Billing and accounting
- Route analysis and optimization
- Carrier management
- Traffic reporting

---

## Huawei Integration

Huawei SBC, eSpace, USG, and IMS equipment support SIPREC with IMS/VoLTE charging vector integration.

### Detected Headers

| Header | Purpose | CDR Field |
| --- | --- | --- |
| `X-Huawei-Session-ID` | Huawei session identifier | `huawei_session_id` |
| `X-Huawei-Call-ID` | Huawei call identifier | `huawei_call_id` |
| `X-Huawei-Correlation-ID` | Correlation identifier | - |
| `X-Huawei-Trunk-ID` | Trunk identifier | `huawei_trunk_id` |
| `X-Huawei-Trunk-Group` | Trunk group name | `huawei_trunk_id` |
| `X-Huawei-Route-ID` | Route identifier | - |
| `X-Huawei-ICID` | IMS Charging ID | `huawei_session_id` |
| `P-Charging-Vector` | IMS charging vector (ICID extraction) | `huawei_session_id` |
| `X-Huawei-Call-Type` | Call type classification | - |
| `X-Huawei-Service-Type` | Service type | - |
| `X-Huawei-Recording-ID` | Recording identifier | - |
| `X-Huawei-Device-ID` | Device identifier | - |
| `X-eSpace-User-ID` | eSpace user ID | - |
| `X-eSpace-Meeting-ID` | eSpace meeting ID | - |
| `X-eSpace-Conf-ID` | eSpace conference ID | - |

### User-Agent Patterns

- `Huawei*`
- `eSpace*`
- `USG*`
- `Eudemon*`
- `Secospace*`

### Stored Metadata

| Metadata Key | Source |
| --- | --- |
| `sip_huawei_session_id` | X-Huawei-Session-ID, X-Huawei-ICID, or P-Charging-Vector ICID |
| `sip_huawei_call_id` | X-Huawei-Call-ID |
| `sip_huawei_trunk_id` | X-Huawei-Trunk-ID or X-Huawei-Trunk-Group |
| `sip_vendor_type` | "huawei" |

### IMS Charging Vector Integration

Huawei IMS equipment uses the P-Charging-Vector header for call correlation. The server automatically extracts the ICID (IMS Charging ID) from this header:

```
P-Charging-Vector: icid-value="1234567890";icid-generated-at=ims.example.com
```

The ICID is extracted and stored as the session identifier for call correlation.

---

## Microsoft Teams/Skype for Business/Lync Integration

Microsoft unified communications platforms (Teams, Skype for Business, Lync) are supported via Direct Routing and SIP trunking through SBCs.

### Detected Headers

| Header | Purpose | CDR Field |
| --- | --- | --- |
| `ms-conversation-id` | Microsoft Conversation ID (primary) | `ms_conversation_id` |
| `X-MS-Conversation-ID` | Alternative Conversation ID | `ms_conversation_id` |
| `X-MS-Call-ID` | Microsoft Call ID | `ms_call_id` |
| `X-MS-Teams-Call-ID` | Teams-specific Call ID | `ms_call_id` |
| `X-MS-Correlation-ID` | Correlation ID for diagnostics | `ms_correlation_id` |
| `X-MS-Skype-Chain-ID` | Skype for Business chain ID | `ms_conversation_id` |
| `X-MS-Teams-Tenant-ID` | Microsoft 365 Tenant ID | - |
| `X-MS-Teams-Meeting-ID` | Teams Meeting ID | - |
| `X-MS-Teams-User-ID` | Teams User ID | - |
| `X-MS-SBC-Host` | SBC hostname | - |
| `X-MS-Mediation-Server` | Mediation server info | - |
| `X-MS-Primary-User-Address` | Primary user SIP address | - |
| `X-MS-Organization-ID` | Organization identifier | - |
| `X-MS-Recording-ID` | Recording identifier | - |
| `X-MS-Compliance-Recording` | Compliance recording flag | - |
| `X-MS-Conf-ID` | Conference ID | - |
| `X-MS-Conference-URI` | Conference URI | - |
| `X-MS-Trunk-Context` | SIP trunk context | - |

### User-Agent Patterns

- `Teams*`
- `Skype*`
- `Lync*`
- `OCS*`
- `Microsoft*`
- `UCMA*`
- `Mediation Server*`
- `MS-*`

### Stored Metadata

| Metadata Key | Source |
| --- | --- |
| `sip_ms_conversation_id` | ms-conversation-id, X-MS-Conversation-ID, or X-MS-Skype-Chain-ID |
| `sip_ms_call_id` | X-MS-Call-ID or X-MS-Teams-Call-ID |
| `sip_ms_correlation_id` | X-MS-Correlation-ID |
| `sip_vendor_type` | "microsoft" |

### Direct Routing Integration

For Microsoft Teams Direct Routing, SIPREC is typically implemented via an SBC (such as AudioCodes) that sits between Teams and the PSTN. The SBC acts as the SIPREC client (SRC) and forwards recording sessions to the SIPREC server (SRS).

Key considerations:
- The SBC extracts Microsoft-specific headers from Teams calls
- ms-conversation-id is the primary correlation identifier
- Compliance recording headers indicate policy-based recording requirements

### Skype for Business / Lync Integration

For on-premises Skype for Business or Lync deployments:
- The Mediation Server or SBA handles SIP trunking
- X-MS-Skype-Chain-ID provides call correlation
- Recording can be integrated via compatible SBCs

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
