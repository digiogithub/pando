"use strict";
// Copyright 2020 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
Object.defineProperty(exports, "__esModule", { value: true });
exports.MetricExporter = void 0;
const core_1 = require("@opentelemetry/core");
const google_auth_library_1 = require("google-auth-library");
// Import directly from this module instead of googleapis to improve bundler tree shaking
const monitoring_1 = require("googleapis/build/src/apis/monitoring");
const transform_1 = require("./transform");
const utils_1 = require("./utils");
const api_1 = require("@opentelemetry/api");
const opentelemetry_resource_util_1 = require("@google-cloud/opentelemetry-resource-util");
const version_1 = require("./version");
// Stackdriver Monitoring v3 only accepts up to 200 TimeSeries per
// CreateTimeSeries call.
const MAX_BATCH_EXPORT_SIZE = 200;
const OT_USER_AGENTS = [
    {
        product: 'opentelemetry-js',
        version: version_1.OT_VERSION,
    },
    {
        product: 'google-cloud-metric-exporter',
        version: version_1.VERSION,
    },
];
const OT_REQUEST_HEADER = {
    'x-opentelemetry-outgoing-request': 0x1,
};
/**
 * Format and sends metrics information to Google Cloud Monitoring.
 */
class MetricExporter {
    _projectId;
    _metricPrefix;
    _auth;
    _disableCreateMetricDescriptors;
    static DEFAULT_METRIC_PREFIX = 'workload.googleapis.com';
    /**
     * Set of OTel metric names that have already had their metric descriptors successfully
     * created
     */
    createdMetricDescriptors = new Set();
    _monitoring;
    constructor(options = {}) {
        this._metricPrefix = options.prefix ?? MetricExporter.DEFAULT_METRIC_PREFIX;
        this._disableCreateMetricDescriptors =
            !!options.disableCreateMetricDescriptors;
        this._auth = new google_auth_library_1.GoogleAuth({
            credentials: options.credentials,
            keyFile: options.keyFile,
            keyFilename: options.keyFilename,
            projectId: options.projectId,
            scopes: ['https://www.googleapis.com/auth/cloud-platform'],
        });
        this._monitoring = (0, monitoring_1.monitoring)({
            version: 'v3',
            rootUrl: 'https://' + (options.apiEndpoint || 'monitoring.googleapis.com:443'),
            headers: OT_REQUEST_HEADER,
            userAgentDirectives: OT_USER_AGENTS.concat(options.userAgent ? [options.userAgent] : []),
        });
        // Start this async process as early as possible. It will be
        // awaited on the first export because constructors are synchronous
        this._projectId = this._auth.getProjectId().catch(err => {
            api_1.diag.error(err);
        });
    }
    /**
     * Implementation for {@link PushMetricExporter.export}.
     * Calls the async wrapper method {@link _exportAsync} and
     * assures no rejected promises bubble up to the caller.
     *
     * @param metrics Metrics to be sent to the Google Cloud Monitoring backend
     * @param resultCallback result callback to be called on finish
     */
    export(metrics, resultCallback) {
        this._exportAsync(metrics).then(resultCallback, err => {
            api_1.diag.error(err.message);
            resultCallback({ code: core_1.ExportResultCode.FAILED, error: err });
        });
    }
    async shutdown() { }
    async forceFlush() { }
    /**
     * Asnyc wrapper for the {@link export} implementation.
     * Writes the current values of all exported {@link MetricRecord}s
     * to the Google Cloud Monitoring backend.
     *
     * @param resourceMetrics Metrics to be sent to the Google Cloud Monitoring backend
     */
    async _exportAsync(resourceMetrics) {
        if (this._projectId instanceof Promise) {
            this._projectId = await this._projectId;
        }
        if (!this._projectId) {
            const error = new Error('expecting a non-blank ProjectID');
            api_1.diag.error(error.message);
            return { code: core_1.ExportResultCode.FAILED, error };
        }
        api_1.diag.debug('Google Cloud Monitoring export');
        const resource = (0, opentelemetry_resource_util_1.mapOtelResourceToMonitoredResource)(resourceMetrics.resource);
        const timeSeries = [];
        for (const scopeMetric of resourceMetrics.scopeMetrics) {
            for (const metric of scopeMetric.metrics) {
                const isRegistered = this._disableCreateMetricDescriptors ||
                    (await this._registerMetricDescriptor(metric));
                if (isRegistered) {
                    timeSeries.push(...(0, transform_1.createTimeSeries)(metric, resource, this._metricPrefix));
                }
            }
        }
        let failure = {
            sendFailed: false,
        };
        for (const batchedTimeSeries of (0, utils_1.partitionList)(timeSeries, MAX_BATCH_EXPORT_SIZE)) {
            try {
                await this._sendTimeSeries(batchedTimeSeries);
            }
            catch (e) {
                const err = asError(e);
                err.message = `Send TimeSeries failed: ${err.message}`;
                failure = { sendFailed: true, error: err };
                api_1.diag.error(err.message);
            }
        }
        if (failure.sendFailed) {
            return { code: core_1.ExportResultCode.FAILED, error: failure.error };
        }
        return { code: core_1.ExportResultCode.SUCCESS };
    }
    /**
     * Returns true if the given metricDescriptor is successfully registered to
     * Google Cloud Monitoring, or the exact same metric has already been
     * registered. Returns false otherwise and should be skipped.
     *
     * @param metric The OpenTelemetry MetricData.
     */
    async _registerMetricDescriptor(metric) {
        const isDescriptorCreated = this.createdMetricDescriptors.has(metric.descriptor.name);
        if (isDescriptorCreated) {
            return true;
        }
        const res = await this._createMetricDescriptorIfNeeded(metric);
        if (res) {
            this.createdMetricDescriptors.add(metric.descriptor.name);
            return true;
        }
        return false;
    }
    /**
     * Returns true if a descriptor already exists within the requested GCP project id;
     * @param descriptor The metric descriptor to check
     * @param projectIdPath The GCP project id path
     * @param authClient The authenticated client which will be used to make the request
     * @returns {boolean}
     */
    async _checkIfDescriptorExists(descriptor, projectIdPath, authClient) {
        try {
            await this._monitoring.projects.metricDescriptors.get({
                name: `${projectIdPath}/metricDescriptors/${descriptor.type}`,
                auth: authClient,
            });
            return true;
        }
        catch (error) {
            return false;
        }
    }
    /**
     * Calls CreateMetricDescriptor in the GCM API for the given InstrumentDescriptor if needed
     * @param metric The OpenTelemetry MetricData.
     * @returns whether or not the descriptor was successfully created
     */
    async _createMetricDescriptorIfNeeded(metric) {
        const authClient = await this._authorize();
        const descriptor = (0, transform_1.transformMetricDescriptor)(metric, this._metricPrefix);
        const projectIdPath = (0, utils_1.mountProjectIdPath)(this._projectId);
        try {
            const descriptorExists = await this._checkIfDescriptorExists(descriptor, projectIdPath, authClient);
            if (!descriptorExists) {
                await this._monitoring.projects.metricDescriptors.create({
                    name: projectIdPath,
                    requestBody: descriptor,
                    auth: authClient,
                });
                api_1.diag.debug('sent metric descriptor', descriptor);
            }
            return true;
        }
        catch (e) {
            const err = asError(e);
            api_1.diag.error('Failed to create metric descriptor: %s', err.message);
            return false;
        }
    }
    async _sendTimeSeries(timeSeries) {
        if (timeSeries.length === 0) {
            return Promise.resolve();
        }
        const authClient = await this._authorize();
        await this._monitoring.projects.timeSeries.create({
            name: (0, utils_1.mountProjectIdPath)(this._projectId),
            requestBody: { timeSeries },
            auth: authClient,
        });
        api_1.diag.debug('sent time series', timeSeries);
    }
    /**
     * Gets the Google Application Credentials from the environment variables
     * and authenticates the client.
     */
    async _authorize() {
        return (await this._auth.getClient());
    }
}
exports.MetricExporter = MetricExporter;
function asError(error) {
    if (error instanceof Error) {
        return error;
    }
    return new Error(String(error));
}
//# sourceMappingURL=monitoring.js.map