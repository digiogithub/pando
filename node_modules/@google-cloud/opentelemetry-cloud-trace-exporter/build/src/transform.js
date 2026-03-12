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
exports.getReadableSpanTransformer = void 0;
const ot = require("@opentelemetry/api");
const types_1 = require("./types");
const opentelemetry_resource_util_1 = require("@google-cloud/opentelemetry-resource-util");
const version_1 = require("./version");
const AGENT_LABEL_KEY = 'g.co/agent';
const AGENT_LABEL_VALUE = `opentelemetry-js ${version_1.OT_VERSION}; google-cloud-trace-exporter ${version_1.VERSION}`;
function getReadableSpanTransformer(projectId, resourceFilter, stringifyArrayAttributes) {
    return span => {
        // @todo get dropped attribute count from sdk ReadableSpan
        const attributes = mergeAttributes(transformAttributes({
            ...span.attributes,
            [AGENT_LABEL_KEY]: AGENT_LABEL_VALUE,
        }, stringifyArrayAttributes), 
        // Add in special g.co/r resource labels
        transformResourceToAttributes(span.resource, projectId, resourceFilter, stringifyArrayAttributes));
        const out = {
            attributes,
            displayName: stringToTruncatableString(span.name),
            links: {
                link: span.links.map(getLinkTransformer(stringifyArrayAttributes)),
            },
            endTime: transformTime(span.endTime),
            startTime: transformTime(span.startTime),
            name: `projects/${projectId}/traces/${span.spanContext().traceId}/spans/${span.spanContext().spanId}`,
            spanKind: transformKind(span.kind),
            spanId: span.spanContext().spanId,
            sameProcessAsParentSpan: { value: !span.spanContext().isRemote },
            status: transformStatus(span.status),
            timeEvents: {
                timeEvent: span.events.map(e => ({
                    time: transformTime(e.time),
                    annotation: {
                        attributes: transformAttributes(e.attributes ?? {}, stringifyArrayAttributes),
                        description: stringToTruncatableString(e.name),
                    },
                })),
            },
        };
        if (span.parentSpanContext?.spanId) {
            out.parentSpanId = span.parentSpanContext?.spanId;
        }
        return out;
    };
}
exports.getReadableSpanTransformer = getReadableSpanTransformer;
function transformStatus(status) {
    switch (status.code) {
        case ot.SpanStatusCode.UNSET:
            return undefined;
        case ot.SpanStatusCode.OK:
            return { code: types_1.Code.OK };
        case ot.SpanStatusCode.ERROR:
            return { code: types_1.Code.UNKNOWN, message: status.message };
        default: {
            exhaust(status.code);
            // TODO: log failed mapping
            return { code: types_1.Code.UNKNOWN, message: status.message };
        }
    }
}
function transformKind(kind) {
    switch (kind) {
        case ot.SpanKind.INTERNAL:
            return types_1.SpanKind.INTERNAL;
        case ot.SpanKind.SERVER:
            return types_1.SpanKind.SERVER;
        case ot.SpanKind.CLIENT:
            return types_1.SpanKind.CLIENT;
        case ot.SpanKind.PRODUCER:
            return types_1.SpanKind.PRODUCER;
        case ot.SpanKind.CONSUMER:
            return types_1.SpanKind.CONSUMER;
        default: {
            exhaust(kind);
            // TODO: log failed mapping
            return types_1.SpanKind.SPAN_KIND_UNSPECIFIED;
        }
    }
}
/**
 * Assert switch case is exhaustive
 */
function exhaust(switchValue) {
    return switchValue;
}
function transformTime(time) {
    return {
        seconds: time[0],
        nanos: time[1],
    };
}
function getLinkTransformer(stringifyArrayAttributes) {
    return link => ({
        attributes: transformAttributes(link.attributes ?? {}, stringifyArrayAttributes),
        spanId: link.context.spanId,
        traceId: link.context.traceId,
        type: types_1.LinkType.UNSPECIFIED,
    });
}
function transformAttributes(attributes, stringifyArrayAttributes) {
    const changedAttributes = transformAttributeNames(attributes);
    return spanAttributesToGCTAttributes(changedAttributes, stringifyArrayAttributes);
}
function spanAttributesToGCTAttributes(attributes, stringifyArrayAttributes) {
    const attributeMap = transformAttributeValues(attributes, stringifyArrayAttributes);
    return {
        attributeMap,
        droppedAttributesCount: Object.keys(attributes).length - Object.keys(attributeMap).length,
    };
}
function mergeAttributes(...attributeList) {
    const attributesOut = {
        attributeMap: {},
        droppedAttributesCount: 0,
    };
    attributeList.forEach(attributes => {
        Object.assign(attributesOut.attributeMap, attributes.attributeMap);
        attributesOut.droppedAttributesCount +=
            attributes.droppedAttributesCount ?? 0;
    });
    return attributesOut;
}
function transformResourceToAttributes(resource, projectId, resourceFilter, stringifyArrayAttributes) {
    const monitoredResource = (0, opentelemetry_resource_util_1.mapOtelResourceToMonitoredResource)(resource);
    const attributes = {};
    if (resourceFilter) {
        Object.keys(resource.attributes)
            .filter(key => resourceFilter.test(key))
            .forEach(key => {
            attributes[key] = resource.attributes[key];
        });
    }
    // global is the "default" so just skip
    if (monitoredResource.type !== 'global') {
        Object.keys(monitoredResource.labels).forEach(labelKey => {
            const key = `g.co/r/${monitoredResource.type}/${labelKey}`;
            attributes[key] = monitoredResource.labels[labelKey];
        });
    }
    return spanAttributesToGCTAttributes(attributes, stringifyArrayAttributes);
}
function transformAttributeValues(attributes, stringifyArrayAttributes) {
    const out = {};
    for (const [key, value] of Object.entries(attributes)) {
        if (value === undefined) {
            continue;
        }
        const attributeValue = valueToAttributeValue(value, stringifyArrayAttributes);
        if (attributeValue !== undefined) {
            out[key] = attributeValue;
        }
    }
    return out;
}
function stringToTruncatableString(value) {
    return { value };
}
function valueToAttributeValue(value, stringifyArrayAttributes) {
    switch (typeof value) {
        case 'number':
            // TODO: Consider to change to doubleValue when available in V2 API.
            return { intValue: String(Math.round(value)) };
        case 'boolean':
            return { boolValue: value };
        case 'string':
            return { stringValue: stringToTruncatableString(value) };
        default:
            if (stringifyArrayAttributes) {
                return { stringValue: stringToTruncatableString(JSON.stringify(value)) };
            }
            // TODO: Handle array types without stringification once API level support is added
            return undefined;
    }
}
const HTTP_ATTRIBUTE_MAPPING = {
    'http.method': '/http/method',
    'http.url': '/http/url',
    'http.host': '/http/host',
    'http.scheme': '/http/client_protocol',
    'http.status_code': '/http/status_code',
    'http.user_agent': '/http/user_agent',
    'http.request_content_length': '/http/request/size',
    'http.response_content_length': '/http/response/size',
    'http.route': '/http/route',
};
function transformAttributeNames(attributes) {
    const out = {};
    for (const [key, value] of Object.entries(attributes)) {
        if (HTTP_ATTRIBUTE_MAPPING[key]) {
            out[HTTP_ATTRIBUTE_MAPPING[key]] = value;
        }
        else {
            out[key] = value;
        }
    }
    return out;
}
//# sourceMappingURL=transform.js.map