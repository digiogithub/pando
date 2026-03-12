"use strict";
/*!
 * Copyright 2015 Google Inc. All Rights Reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
Object.defineProperty(exports, "__esModule", { value: true });
exports.Entry = exports.TRACE_SAMPLED_KEY = exports.TRACE_KEY = exports.SPAN_ID_KEY = exports.SOURCE_LOCATION_KEY = exports.OPERATION_KEY = exports.LABELS_KEY = exports.INSERT_ID_KEY = void 0;
// eslint-disable-next-line @typescript-eslint/no-var-requires
const EventId = require('eventid');
const extend = require("extend");
const common_1 = require("./utils/common");
const http_request_1 = require("./utils/http-request");
const context_1 = require("./utils/context");
const eventId = new EventId();
exports.INSERT_ID_KEY = 'logging.googleapis.com/insertId';
exports.LABELS_KEY = 'logging.googleapis.com/labels';
exports.OPERATION_KEY = 'logging.googleapis.com/operation';
exports.SOURCE_LOCATION_KEY = 'logging.googleapis.com/sourceLocation';
exports.SPAN_ID_KEY = 'logging.googleapis.com/spanId';
exports.TRACE_KEY = 'logging.googleapis.com/trace';
exports.TRACE_SAMPLED_KEY = 'logging.googleapis.com/trace_sampled';
/**
 * Create an entry object to define new data to insert into a meta.
 *
 * Note, {@link https://cloud.google.com/logging/quotas|Cloud Logging Quotas and limits}
 * dictates that the maximum log entry size, including all
 * {@link https://cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry|LogEntry Resource properties},
 * cannot exceed approximately 256 KB.
 *
 * See {@link https://cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry|LogEntry JSON representation}
 *
 * @class
 *
 * @param {?object} [metadata] See a
 *     [LogEntry
 * Resource](https://cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry).
 * @param {object|string} data The data to use as the value for this log
 *     entry.
 *
 *     If providing an object, these value types are supported:
 *     - `String`
 *     - `Number`
 *     - `Boolean`
 *     - `Buffer`
 *     - `Object`
 *     - `Array`
 *
 *     Any other types are stringified with `String(value)`.
 *
 * @example
 * ```
 * const {Logging} = require('@google-cloud/logging');
 * const logging = new Logging();
 * const syslog = logging.log('syslog');
 *
 * const metadata = {
 *   resource: {
 *     type: 'gce_instance',
 *     labels: {
 *       zone: 'global',
 *       instance_id: '3'
 *     }
 *   }
 * };
 *
 * const entry = syslog.entry(metadata, {
 *   delegate: 'my_username'
 * });
 *
 * syslog.alert(entry, (err, apiResponse) => {
 *   if (!err) {
 *     // Log entry inserted successfully.
 *   }
 * });
 *
 * //-
 * // You will also receive `Entry` objects when using
 * // Logging#getEntries() and Log#getEntries().
 * //-
 * logging.getEntries((err, entries) => {
 *   if (!err) {
 *     // entries[0].data = The data value from the log entry.
 *   }
 * });
 * ```
 */
class Entry {
    constructor(metadata, data) {
        /**
         * @name Entry#metadata
         * @type {object}
         * @property {Date} timestamp
         * @property {number} insertId
         */
        this.metadata = extend({
            timestamp: new Date(),
        }, metadata);
        // JavaScript date has a very coarse granularity (millisecond), which makes
        // it quite likely that multiple log entries would have the same timestamp.
        // The Logging API doesn't guarantee to preserve insertion order for entries
        // with the same timestamp. The service does use `insertId` as a secondary
        // ordering for entries with the same timestamp. `insertId` needs to be
        // globally unique (within the project) however.
        //
        // We use a globally unique monotonically increasing EventId as the
        // insertId.
        this.metadata.insertId = this.metadata.insertId || eventId.new();
        /**
         * @name Entry#data
         * @type {object}
         */
        this.data = data;
    }
    /**
     * Serialize an entry to the format the API expects. Read more:
     * https://cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry
     *
     * @param {object} [options] Configuration object.
     * @param {boolean} [options.removeCircular] Replace circular references in an
     *     object with a string value, `[Circular]`.
     * @param {string} [projectId] GCP Project ID.
     */
    toJSON(options = {}, projectId = '') {
        const entry = extend(true, {}, this.metadata);
        // Format log message
        if (this.isObject(this.data)) {
            entry.jsonPayload = (0, common_1.objToStruct)(this.data, {
                removeCircular: !!options.removeCircular,
                stringify: true,
            });
        }
        else if (typeof this.data === 'string') {
            entry.textPayload = this.data;
        }
        // Format log timestamp
        if (entry.timestamp instanceof Date) {
            entry.timestamp = (0, common_1.toNanosAndSecondsObj)(entry.timestamp);
        }
        else if (typeof entry.timestamp === 'string') {
            entry.timestamp = (0, common_1.zuluToDateObj)(entry.timestamp);
        }
        // Format httpRequest
        const req = this.metadata.httpRequest;
        if ((0, http_request_1.isRawHttpRequest)(req)) {
            entry.httpRequest = (0, http_request_1.makeHttpRequestData)(req);
        }
        // Format trace and span
        const traceContext = this.extractTraceContext(projectId);
        if (traceContext) {
            if (!this.metadata.trace && traceContext.trace)
                entry.trace = traceContext.trace;
            if (!this.metadata.spanId && traceContext.spanId)
                entry.spanId = traceContext.spanId;
            if (this.metadata.traceSampled === undefined)
                entry.traceSampled = traceContext.traceSampled;
        }
        return entry;
    }
    /**
     * Serialize an entry to a standard format for any transports, e.g. agents.
     * Read more: https://cloud.google.com/logging/docs/structured-logging
     */
    toStructuredJSON(projectId = '', useMessageField = true) {
        const meta = this.metadata;
        // Mask out the keys that need to be renamed.
        /* eslint-disable @typescript-eslint/no-unused-vars */
        const { textPayload, jsonPayload, insertId, trace, spanId, traceSampled, operation, sourceLocation, labels, ...validKeys } = meta;
        /* eslint-enable @typescript-eslint/no-unused-vars */
        let entry = extend(true, {}, validKeys);
        // Re-map keys names.
        entry[exports.LABELS_KEY] = meta.labels
            ? Object.assign({}, meta.labels)
            : undefined;
        entry[exports.INSERT_ID_KEY] = meta.insertId || undefined;
        entry[exports.TRACE_KEY] = meta.trace || undefined;
        entry[exports.SPAN_ID_KEY] = meta.spanId || undefined;
        entry[exports.TRACE_SAMPLED_KEY] =
            'traceSampled' in meta && meta.traceSampled !== null
                ? meta.traceSampled
                : undefined;
        // Format log payload.
        const data = this.data ||
            meta.textPayload ||
            meta.jsonPayload ||
            meta.protoPayload ||
            undefined;
        if (useMessageField) {
            /** If useMessageField is set, we add the payload to {@link StructuredJson#message} field.*/
            entry.message = data;
        }
        else {
            /** useMessageField is false, we add the structured payload to {@link StructuredJson} key-value map.
             * It could be especially useful for serverless environments like Cloud Run/Functions when stdout transport is used.
             * Note that text still added to {@link StructuredJson#message} field for text payload since it does not have fields within. */
            if (data !== undefined && data !== null) {
                if (this.isObject(data)) {
                    entry = extend(true, {}, entry, data);
                }
                else if (typeof data === 'string') {
                    entry.message = data;
                }
                else {
                    entry.message = JSON.stringify(data);
                }
            }
        }
        // Format timestamp
        if (meta.timestamp instanceof Date) {
            entry.timestamp = (0, common_1.toNanosAndSecondsObj)(meta.timestamp);
        }
        // Format httprequest
        const req = meta.httpRequest;
        if ((0, http_request_1.isRawHttpRequest)(req)) {
            entry.httpRequest = (0, http_request_1.makeHttpRequestData)(req);
        }
        // Detected trace context from OpenTelemetry context or http headers if applicable.
        const traceContext = this.extractTraceContext(projectId);
        if (traceContext) {
            if (!entry[exports.TRACE_KEY] && traceContext.trace)
                entry[exports.TRACE_KEY] = traceContext.trace;
            if (!entry[exports.SPAN_ID_KEY] && traceContext.spanId)
                entry[exports.SPAN_ID_KEY] = traceContext.spanId;
            if (entry[exports.TRACE_SAMPLED_KEY] === undefined)
                entry[exports.TRACE_SAMPLED_KEY] = traceContext.traceSampled;
        }
        return entry;
    }
    /**
     * extractTraceContext extracts trace and span information from OpenTelemetry
     * span context or raw HTTP request headers.
     * @private
     */
    extractTraceContext(projectId) {
        // Extract trace context from OpenTelemetry span context.
        const otelContext = (0, context_1.getContextFromOtelContext)(projectId);
        if (otelContext)
            return otelContext;
        // Extract trace context from http request headers.
        const rawReq = this.metadata.httpRequest;
        if (rawReq && 'headers' in rawReq) {
            return (0, context_1.getOrInjectContext)(rawReq, projectId, false);
        }
        return null;
    }
    /**
     * Create an Entry object from an API response, such as `entries:list`.
     *
     * @private
     *
     * @param {object} entry An API representation of an entry. See a
     *     {@link https://cloud.google.com/logging/docs/reference/v2/rest/v2/LogEntry| LogEntry}.
     * @returns {Entry}
     */
    static fromApiResponse_(entry) {
        let data = entry[entry.payload];
        if (entry.payload === 'jsonPayload') {
            data = (0, common_1.structToObj)(data);
        }
        const serializedEntry = new Entry(entry, data);
        if (entry.timestamp) {
            let ms = Number(entry.timestamp.seconds) * 1000;
            ms += Number(entry.timestamp.nanos) / 1e6;
            serializedEntry.metadata.timestamp = new Date(ms);
        }
        return serializedEntry;
    }
    /**
     * Determines whether `value` is a JavaScript object.
     * @param value The value to be checked
     * @returns true if `value` is a JavaScript object, false otherwise
     */
    isObject(value) {
        return Object.prototype.toString.call(value) === '[object Object]';
    }
}
exports.Entry = Entry;
//# sourceMappingURL=entry.js.map