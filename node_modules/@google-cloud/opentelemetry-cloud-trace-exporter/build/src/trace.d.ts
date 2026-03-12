import { ExportResult } from '@opentelemetry/core';
import { ReadableSpan, SpanExporter } from '@opentelemetry/sdk-trace-base';
import { TraceExporterOptions } from './external-types';
/**
 * Format and sends span information to Google Cloud Trace.
 */
export declare class TraceExporter implements SpanExporter {
    private _projectId;
    private _stringifyArrayAttributes;
    private readonly _auth;
    private _traceServiceClient?;
    private _resourceFilter?;
    private _apiEndpoint;
    constructor(options?: TraceExporterOptions);
    /**
     * Publishes a list of spans to Google Cloud Trace.
     * @param spans The list of spans to transmit to Google Cloud Trace
     */
    export(spans: ReadableSpan[], resultCallback: (result: ExportResult) => void): Promise<void>;
    shutdown(): Promise<void>;
    /**
     * Sends new spans to new or existing traces in the Google Cloud Trace format to the
     * service.
     * @param spans
     */
    private _batchWriteSpans;
    /**
     * If the rpc client is not already initialized,
     * authenticates with google credentials and initializes the rpc client
     */
    private _getClient;
}
