export interface TraceExporterOptions {
    /**
     * Google Cloud Platform project ID where your traces will be stored.
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
     * A RegExp used to determine which resource attributes are exported,
     * attributes that match will be included as span labels.
     * If not specified, most resource attributes are ignored.
     */
    resourceFilter?: RegExp;
    /**
     * The endpoint of the cloud trace service. Defaults to
     * cloudtrace.googleapis.com.
     */
    apiEndpoint?: string;
    /**
     * If enabled, array type Span attributes are JSON stringified before
     * exporting. Default: false, which means array attributes are simply
     * ignored and not exported
     */
    stringifyArrayAttributes?: boolean;
}
export interface Credentials {
    client_email?: string;
    private_key?: string;
}
