export interface ExporterOptions {
    /**
     * Google Cloud Platform project ID where your metrics will be stored.
     * This is optional and will be inferred from your authentication
     * credentials or from the GCP environment when not specified.
     */
    projectId?: string;
    /**
     * Path to a .json, .pem, or .p12 key file. This is optional and
     * authentication keys will be inferred from the environment if you
     * are running on GCP.
     */
    keyFilename?: string;
    /**
     * Path to a .json, .pem, or .p12 key file. This is optional and
     * authentication keys will be inferred from the environment if you
     * are running on GCP.
     */
    keyFile?: string;
    /**
     * Object containing client_email and private_key properties
     */
    credentials?: Credentials;
    /**
     * Prefix prepended to OpenTelemetry metric names when writing to Cloud Monitoring. See
     * https://cloud.google.com/monitoring/custom-metrics#identifier for more details.
     *
     * Optional, default is `workload.googleapis.com`.
     */
    prefix?: string;
    /**
     * The api endpoint of the cloud monitoring service. Defaults to
     * monitoring.googleapis.com:443.
     */
    apiEndpoint?: string;
    /**
     * Disable calling CreateMetricDescriptor before sending time series to Cloud Monitoring.
     * Metric descriptors will be
     * {@link https://cloud.google.com/monitoring/custom-metrics/creating-metrics#auto-creation | auto-created}
     * if needed, but may be missing descriptions. This can prevent hitting a rate limit in
     * Cloud Monitoring when a large number of clients are all started up at the same time.
     */
    disableCreateMetricDescriptors?: boolean;
    /**
     * Add a custom user agent and version strings to all monitoring exports
     */
    userAgent?: {
        product: string;
        version: string;
    };
}
export interface Credentials {
    client_email?: string;
    private_key?: string;
}
