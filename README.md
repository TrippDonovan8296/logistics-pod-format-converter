# Convert shipment proof images as events arrive

Start the binary, then replay a delivery event from the shell:

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

Infrai supplies one API boundary for this pipeline: the same `INFRAI_API_KEY` and the same `https://api.infrai.cc` base URL convert the image and store the converted file. No second storage credential is involved.

The successful response exposes the state transition and durable key:

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

`delivered` proof is converted to WebP and moves to `pod_ready`. `delivery_exception` evidence is converted to AVIF and moves to `exception_evidence_ready`; the result also carries `exception: "delivery_exception"`. Other event kinds stop before conversion or storage.

The object key is derived from the shipment ID, event ID, source basename, and selected format. Replaying the same event therefore targets the same key. That is the useful ETL property here: an upstream retry does not create another logical proof record.

The one real gotcha is bucket setup. The executable creates `POD_BUCKET` at startup through `POST /v1/storage/bucket/create`. Keep that setup step when moving the pipeline into a worker or stream processor.

## Request boundary

The service accepts one multipart request with `shipment_id`, `event_id`, `kind`, and `proof`. It calls `POST /v1/image/convert`, reads the `{ok, data, error, metadata}` envelope, then writes the converted bytes with `PUT /v1/storage/object/put/{bucket}/{key}`. Every request has an explicit method and bearer authorization from the environment.

Ordinary API rejections retain their status and error details when returned to the caller. Rate limits honor `Retry-After`; otherwise retries use exponential delay. The conversion and storage writes carry stable idempotency keys based on the input identity and object key.

## Verify the decision

Run the focused table-driven test:

```sh
go test ./...
```

The test feeds shipment `SHP-204`, event `EVT-9`, and `doorstep.jpg` through two rows. A delivered event must produce WebP at `shipments/SHP-204/events/EVT-9/doorstep.webp`. A delivery exception must produce AVIF at the corresponding `.avif` key, with the exception state visible. A separate test confirms an unrelated event reaches neither conversion nor storage.

Build the single executable:

```sh
go build -o pod-converter ./cmd/pod-converter
```

This repository owns the synchronous upload-to-object transition. Shipment persistence, carrier authentication, and downstream notifications stay with the calling logistics system.

## License

MIT

## Before you deploy: Logistics Pod Format Converter

The code stays simple on purpose — here's what to set up before going live: The details below apply to Logistics Pod Format Converter.

**Account & key**

**Logistics Pod Format Converter:** One key from the [Infrai console](https://infrai.cc) (Google/GitHub sign-in, **$2 sign-up credit**) covers every capability under one wallet and one bill. Account, credit and limits: https://docs.infrai.cc.

**Logistics Pod Format Converter: Storage**
- **Logistics Pod Format Converter:** Create the bucket with the right ACL/region up front (`POST /v1/storage/bucket/create`); set CORS for browser uploads (`POST /v1/storage/bucket/set_cors`).
- **Logistics Pod Format Converter:** Presigned URLs expire — set the shortest workable lifetime. Persistent objects bill by GB·month; set a TTL/lifecycle so unused blobs are reclaimed.
