import { MetricData } from '@opentelemetry/sdk-metrics';
import { MonitoredResource } from '@google-cloud/opentelemetry-resource-util';
import { MetricDescriptor, TimeSeries } from './types';
/**
 *
 * @param metric the MetricData to create a descriptor for
 * @param metricPrefix prefix to add to metric names
 * @param displayNamePrefix prefix to add to display name in the descriptor
 * @returns the GCM MetricDescriptor or null if the MetricData was empty
 */
export declare function transformMetricDescriptor(metric: MetricData, metricPrefix: string): MetricDescriptor;
/**
 * Converts metric's timeseries to a TimeSeries, so that metric can be
 * uploaded to GCM.
 */
export declare function createTimeSeries(metric: MetricData, resource: MonitoredResource, metricPrefix: string): TimeSeries[];
declare function normalizeLabelKey(key: string): string;
export declare const _TEST_ONLY: {
    normalizeLabelKey: typeof normalizeLabelKey;
};
export {};
