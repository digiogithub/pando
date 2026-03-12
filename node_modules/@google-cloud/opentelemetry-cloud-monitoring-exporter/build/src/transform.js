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
exports._TEST_ONLY = exports.createTimeSeries = exports.transformMetricDescriptor = void 0;
const sdk_metrics_1 = require("@opentelemetry/sdk-metrics");
const api_1 = require("@opentelemetry/api");
const types_1 = require("./types");
const utils_1 = require("./utils");
const path = require("path");
const precise_date_1 = require("@google-cloud/precise-date");
/**
 *
 * @param metric the MetricData to create a descriptor for
 * @param metricPrefix prefix to add to metric names
 * @param displayNamePrefix prefix to add to display name in the descriptor
 * @returns the GCM MetricDescriptor or null if the MetricData was empty
 */
function transformMetricDescriptor(metric, metricPrefix) {
    const { descriptor: { name, description, unit }, } = metric;
    return {
        type: transformMetricType(metricPrefix, name),
        description,
        displayName: name,
        metricKind: transformMetricKind(metric),
        valueType: transformValueType(metric),
        unit,
        labels: transformLabelDescriptors(metric),
    };
}
exports.transformMetricDescriptor = transformMetricDescriptor;
function transformLabelDescriptors(metric) {
    if (metric.dataPoints.length === 0) {
        return [];
    }
    const attrs = metric.dataPoints[0].attributes;
    return Object.keys(attrs).map(key => ({
        key: normalizeLabelKey(key),
        description: '',
    }));
}
/** Transforms Metric type. */
function transformMetricType(metricPrefix, name) {
    return path.posix.join(metricPrefix, name);
}
/** Transforms a OpenTelemetry instrument type to a GCM MetricKind. */
function transformMetricKind(metric) {
    switch (metric.dataPointType) {
        case sdk_metrics_1.DataPointType.SUM:
            return metric.isMonotonic ? types_1.MetricKind.CUMULATIVE : types_1.MetricKind.GAUGE;
        case sdk_metrics_1.DataPointType.GAUGE:
            return types_1.MetricKind.GAUGE;
        case sdk_metrics_1.DataPointType.HISTOGRAM:
        case sdk_metrics_1.DataPointType.EXPONENTIAL_HISTOGRAM:
            return types_1.MetricKind.CUMULATIVE;
        default:
            exhaust(metric);
            // No logging needed as it will be done in transformPoints()
            return types_1.MetricKind.UNSPECIFIED;
    }
}
/** Transforms a OpenTelemetry ValueType to a GCM ValueType. */
function transformValueType(metric) {
    const { dataPointType, descriptor: { valueType }, } = metric;
    switch (dataPointType) {
        case sdk_metrics_1.DataPointType.HISTOGRAM:
        case sdk_metrics_1.DataPointType.EXPONENTIAL_HISTOGRAM:
            return types_1.ValueType.DISTRIBUTION;
        case sdk_metrics_1.DataPointType.GAUGE:
        case sdk_metrics_1.DataPointType.SUM:
            // handle below
            break;
        default:
            exhaust(dataPointType);
            // No logging needed as it will be done in transformPoints()
            return types_1.ValueType.VALUE_TYPE_UNSPECIFIED;
    }
    switch (valueType) {
        case api_1.ValueType.DOUBLE:
            return types_1.ValueType.DOUBLE;
        case api_1.ValueType.INT:
            return types_1.ValueType.INT64;
        default:
            exhaust(valueType);
            api_1.diag.info('Encountered unexpected value type %s', valueType);
            return types_1.ValueType.VALUE_TYPE_UNSPECIFIED;
    }
}
/**
 * Converts metric's timeseries to a TimeSeries, so that metric can be
 * uploaded to GCM.
 */
function createTimeSeries(metric, resource, metricPrefix) {
    const metricKind = transformMetricKind(metric);
    const valueType = transformValueType(metric);
    return transformPoints(metric, metricPrefix).map(({ point, metric }) => ({
        metric,
        resource,
        metricKind,
        valueType,
        points: [point],
    }));
}
exports.createTimeSeries = createTimeSeries;
function transformMetric(point, instrumentDescriptor, metricPrefix) {
    const type = transformMetricType(metricPrefix, instrumentDescriptor.name);
    const labels = {};
    Object.keys(point.attributes).forEach(key => {
        labels[normalizeLabelKey(key)] = `${point.attributes[key]}`;
    });
    return { type, labels };
}
/**
 * Transform timeseries's point, so that metric can be uploaded to GCM.
 */
function transformPoints(metric, metricPrefix) {
    switch (metric.dataPointType) {
        case sdk_metrics_1.DataPointType.SUM:
        case sdk_metrics_1.DataPointType.GAUGE:
            return metric.dataPoints.map(dataPoint => ({
                metric: transformMetric(dataPoint, metric.descriptor, metricPrefix),
                point: {
                    value: transformNumberValue(metric.descriptor.valueType, dataPoint.value),
                    interval: {
                        // Add start time for non-gauge points
                        ...(metric.dataPointType === sdk_metrics_1.DataPointType.SUM && metric.isMonotonic
                            ? {
                                startTime: new precise_date_1.PreciseDate(dataPoint.startTime).toISOString(),
                            }
                            : null),
                        endTime: new precise_date_1.PreciseDate(dataPoint.endTime).toISOString(),
                    },
                },
            }));
        case sdk_metrics_1.DataPointType.HISTOGRAM:
            return metric.dataPoints.map(dataPoint => ({
                metric: transformMetric(dataPoint, metric.descriptor, metricPrefix),
                point: {
                    value: transformHistogramValue(dataPoint.value),
                    interval: {
                        startTime: new precise_date_1.PreciseDate(dataPoint.startTime).toISOString(),
                        endTime: new precise_date_1.PreciseDate(dataPoint.endTime).toISOString(),
                    },
                },
            }));
        case sdk_metrics_1.DataPointType.EXPONENTIAL_HISTOGRAM:
            return metric.dataPoints.map(dataPoint => ({
                metric: transformMetric(dataPoint, metric.descriptor, metricPrefix),
                point: {
                    value: transformExponentialHistogramValue(dataPoint.value),
                    interval: {
                        startTime: new precise_date_1.PreciseDate(dataPoint.startTime).toISOString(),
                        endTime: new precise_date_1.PreciseDate(dataPoint.endTime).toISOString(),
                    },
                },
            }));
        default:
            exhaust(metric);
            api_1.diag.info('Encountered unexpected dataPointType=%s, dropping %s points', metric.dataPointType, metric.dataPoints.length);
            break;
    }
    return [];
}
/** Transforms a OpenTelemetry Point's value to a GCM Point value. */
function transformNumberValue(valueType, value) {
    if (valueType === api_1.ValueType.INT) {
        return { int64Value: value.toString() };
    }
    else if (valueType === api_1.ValueType.DOUBLE) {
        return { doubleValue: value };
    }
    exhaust(valueType);
    throw Error(`unsupported value type: ${valueType}`);
}
function transformHistogramValue(value) {
    return {
        distributionValue: {
            // sumOfSquaredDeviation param not aggregated
            count: value.count.toString(),
            mean: value.count && value.sum ? value.sum / value.count : 0,
            bucketOptions: {
                explicitBuckets: { bounds: value.buckets.boundaries },
            },
            bucketCounts: (0, utils_1.numbersToStrings)(value.buckets.counts),
        },
    };
}
function transformExponentialHistogramValue(value) {
    // Adapated from reference impl in Go which has more explanatory comments
    // https://github.com/GoogleCloudPlatform/opentelemetry-operations-go/blob/v1.8.0/exporter/collector/metrics.go#L582
    const underflow = value.zeroCount +
        value.negative.bucketCounts.reduce((prev, current) => prev + current, 0);
    const bucketCounts = [
        underflow,
        ...value.positive.bucketCounts,
        0, // overflow bucket is always empty
    ];
    let bucketOptions;
    if (value.positive.bucketCounts.length === 0) {
        bucketOptions = {
            explicitBuckets: { bounds: [] },
        };
    }
    else {
        const growthFactor = (0, utils_1.exp2)((0, utils_1.exp2)(-value.scale));
        const scale = Math.pow(growthFactor, value.positive.offset);
        bucketOptions = {
            exponentialBuckets: {
                growthFactor,
                scale,
                numFiniteBuckets: bucketCounts.length - 2,
            },
        };
    }
    const mean = value.sum === undefined || value.count === 0 ? 0 : value.sum / value.count;
    return {
        distributionValue: {
            // sumOfSquaredDeviation param not aggregated
            count: value.count.toString(),
            mean,
            bucketOptions,
            bucketCounts: (0, utils_1.numbersToStrings)(bucketCounts),
        },
    };
}
function normalizeLabelKey(key) {
    // Replace characters which are not Letter or Decimal_Number unicode category with "_", see
    // https://developer.mozilla.org/en-US/docs/Web/JavaScript/Guide/Regular_Expressions/Unicode_Property_Escapes
    //
    // Reimplementation of reference impl in Go:
    // https://github.com/GoogleCloudPlatform/opentelemetry-operations-go/blob/e955c204f4f2bfdc92ff0ad52786232b975efcc2/exporter/metric/metric.go#L595-L604
    let sanitized = key.replace(/[^\p{Letter}\p{Decimal_Number}_]/gu, '_');
    if (sanitized[0].match(/\p{Decimal_Number}/u)) {
        sanitized = 'key_' + sanitized;
    }
    return sanitized;
}
/**
 * Assert switch case is exhaustive
 */
function exhaust(switchValue) {
    return switchValue;
}
exports._TEST_ONLY = {
    normalizeLabelKey,
};
//# sourceMappingURL=transform.js.map