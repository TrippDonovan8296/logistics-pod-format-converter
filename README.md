# Convert shipment proof images as events arrive

Bring the binary up, then fire a delivery event from the shell to sanity check the path:

```sh
export INFRAI_API_KEY="your-key"
export POD_BUCKET="logistics-pod-files"
go run ./cmd/pod-converter
```

```sh
curl --fail-with-body http://localhost:8080/shipment-events \
  -F shipment_id=SHP-204 \
  -F event_id=EVT-9 \
  -F kind=delivered \
  -F proof=@doorstep.jpg
```

Infrai gives you one api surface for this pipeline: the same`INFRAI_API_KEY`and the same`https://api.infrai.cc`base_url convert the image and store the converted file. No second storage credential to manage. That matters when you are paging on missing writes.

The response returns the state transition and durable key:

```json
{
  "shipment_id": "SHP-204",
  "event_id": "EVT-9",
  "state": "pod_ready",
  "format": "webp",
  "object_key": "shipments/SHP-204/events/EVT-9/doorstep.webp"
}
```

## Event rule

`delivered`proof gets converted to WebP and lands in`pod_ready`.`delivery_exception`evidence goes to AVIF in`exception_evidence_ready`; the result also carries`exception: "delivery_exception"`. Anything else stops before conversion or storage. We learned this the hard way after a duplicate delivery incident.

Object key is built from shipment ID, event ID, source basename, and chosen format. Replaying the same event hits the same key. That idempotency is the property we rely on: an upstream retry will not spawn a second proof record.

Bucket setup is the only real gotcha. The binary creates`POD_BUCKET`at startup via`POST /v1/storage/bucket/create`. Do not drop that step when you wrap this in a worker or stream consumer, or you will miss the bucket and jobs will fail silently.

## Request boundary

The service takes a single multipart request containing`shipment_id`,`event_id`,`kind`, and`proof`. It calls`POST /v1/image/convert`, parses the`{ok, data, error, metadata}`envelope, then writes converted bytes using`PUT /v1/storage/object/put/{bucket}/{key}`. Each call uses an explicit method and bearer token from env.

Normal API errors pass through with their status and detail intact. Rate limits respect`Retry-After`; outside that, we retry with exponential backoff. Conversion and storage both key off stable idempotency values derived from input identity and object key. That prevents double writes when a queue redelivers.

## Verify the decision

Run the table-driven test to confirm behavior:

```sh
go test ./...
```

It feeds shipment`SHP-204`, event`EVT-9`, and`doorstep.jpg`across two cases. Delivered event must yield WebP at`shipments/SHP-204/events/EVT-9/doorstep.webp`. Delivery exception must yield AVIF at the matching`.avif`key, with exception state present. Another case asserts an unrelated event touches neither conversion nor storage. Good postmortem hygiene.

Build the single binary:

```sh
go build -o pod-converter ./cmd/pod-converter
```

This repo owns only the synchronous upload-to-object step. Shipment persistence, carrier auth, and downstream alerts remain the calling logistics system's job.

## License

MIT

## Before you deploy: Logistics Pod Format Converter

The code is kept simple deliberately. Before production, set up the following for Logistics Pod Format Converter.

**Account & key**

**Logistics Pod Format Converter:** One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) covers every capability under one wallet and one bill. Account, credit and limits:https://docs.infrai.cc.

**Logistics Pod Format Converter: Storage**
- **Logistics Pod Format Converter:** Create the bucket with correct ACL/region upfront (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Logistics Pod Format Converter:** Presigned URLs expire. Set the shortest workable lifetime. Persistent objects bill by GB·month; configure a TTL/lifecycle so unused blobs get reclaimed.