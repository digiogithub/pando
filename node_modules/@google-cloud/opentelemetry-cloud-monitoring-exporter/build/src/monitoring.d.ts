import { PushMetricExporter, ResourceMetrics } from '@opentelemetry/sdk-metrics';
import { ExportResult } from '@opentelemetry/core';
import { ExporterOptions } from './external-types';
/**
 * Format and sends metrics information to Google Cloud Monitoring.
 */
export declare class MetricExporter implements PushMetricExporter {
    private _projectId;
    private readonly _metricPrefix;
    private readonly _auth;
    private readonly _disableCreateMetricDescriptors;
    static readonly DEFAULT_METRIC_PREFIX: string;
    /**
     * Set of OTel metric names that have already had their metric descriptors successfully
     * created
     */
    private createdMetricDescriptors;
    private _monitoring;
    constructor(options?: ExporterOptions);
    /**
     * Implementation for {@link PushMetricExporter.export}.
     * Calls the async wrapper method {@link _exportAsync} and
     * assures no rejected promises bubble up to the caller.
     *
     * @param metrics Metrics to be sent to the Google Cloud Monitoring backend
     * @param resultCallback result callback to be called on finish
     */
    export(metrics: ResourceMetrics, resultCallback: (result: ExportResult) => void): void;
    shutdown(): Promise<void>;
    forceFlush(): Promise<void>;
    /**
     * Asnyc wrapper for the {@link export} implementation.
     * Writes the current values of all exported {@link MetricRecord}s
     * to the Google Cloud Monitoring backend.
     *
     * @param resourceMetrics Metrics to be sent to the Google Cloud Monitoring backend
     */
    private _exportAsync;
    /**
     * Returns true if the given metricDescriptor is successfully registered to
     * Google Cloud Monitoring, or the exact same metric has already been
     * registered. Returns false otherwise and should be skipped.
     *
     * @param metric The OpenTelemetry MetricData.
     */
    private _registerMetricDescriptor;
    /**
     * Returns true if a descriptor already exists within the requested GCP project id;
     * @param descriptor The metric descriptor to check
     * @param projectIdPath The GCP project id path
     * @param authClient The authenticated client which will be used to make the request
     * @returns {boolean}
     */
    private _checkIfDescriptorExists;
    /**
     * Calls CreateMetricDescriptor in the GCM API for the given InstrumentDescriptor if needed
     * @param metric The OpenTelemetry MetricData.
     * @returns whether or not the descriptor was successfully created
     */
    private _createMetricDescriptorIfNeeded;
    private _sendTimeSeries;
    /**
     * Gets the Google Application Credentials from the environment variables
     * and authenticates the client.
     */
    private _authorize;
}
