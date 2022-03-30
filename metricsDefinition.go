package main

import (
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/servicequotas"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	failCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "total_aws_api_call_failed_count",
			Help: "Total aws api call failed count",
		},
		[]string{"api"},
	)

	totalCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "total_aws_api_call_count",
			Help: "Total aws api call count",
		},
		[]string{"api"},
	)

	code = [][]string{
		{"s3", "L-DC2B2D3D"},
		{"acm", "L-F141DD1D"},
		{"cloudfront", "L-24B04930"},
		{"cloudfront", "L-08884E5C"},
	}

	serviceQuotasS3FilterChanged = servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaInput{
		ServiceCode: &code[0][0],
		QuotaCode:   &code[0][1],
		Status:      "CASE_CLOSED",
	}
	serviceQuotasS3FilterDefault = servicequotas.GetAWSDefaultServiceQuotaInput{
		ServiceCode: &code[0][0],
		QuotaCode:   &code[0][1],
	}

	serviceQuotasACMFilterChanged = servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaInput{
		ServiceCode: &code[1][0],
		QuotaCode:   &code[1][1],
		Status:      "CASE_CLOSED",
	}

	serviceQuotasACMFilterDefault = servicequotas.GetAWSDefaultServiceQuotaInput{
		ServiceCode: &code[1][0],
		QuotaCode:   &code[1][1],
	}

	serviceQuotasCloudFrontDistributionsFilterChanged = servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaInput{
		ServiceCode: &code[2][0],
		QuotaCode:   &code[2][1],
		Status:      "CASE_CLOSED",
	}

	serviceQuotasCloudFrontDistributionsFilterDefault = servicequotas.GetAWSDefaultServiceQuotaInput{
		ServiceCode: &code[2][0],
		QuotaCode:   &code[2][1],
	}

	serviceQuotasCloudFrontOAIFilterChanged = servicequotas.ListRequestedServiceQuotaChangeHistoryByQuotaInput{
		ServiceCode: &code[3][0],
		QuotaCode:   &code[3][1],
		Status:      "CASE_CLOSED",
	}

	serviceQuotasCloudFrontOAIFilterDefault = servicequotas.GetAWSDefaultServiceQuotaInput{
		ServiceCode: &code[3][0],
		QuotaCode:   &code[3][1],
	}

	s3Inout      = s3.ListBucketsInput{}
	s3CurrentVec = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "total_buckets_usage_per_region_current",
			Help: "Total buckets usage per region current",
		},
		[]string{"account", "region"},
	)
	s3LimitedVec = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "total_buckets_usage_per_region_limited",
			Help: "Total buckets usage per region limited",
		},
		[]string{"account", "region"},
	)

	acmInput      = acm.ListCertificatesInput{}
	acmCurrentVec = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "total_certificates_usage_per_region_current",
			Help: "Total certificates usage per region current",
		},
		[]string{"account", "region"},
	)
	acmLimitedVec = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "total_certificates_usage_per_region_limited",
			Help: "Total certificates usage per region limited",
		},
		[]string{"account", "region"},
	)

	cfInput                   = cloudfront.ListDistributionsInput{}
	cfDistributionsCurrentVec = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "total_cloudfront_distributions_usage_current",
			Help: "Total cloudfront distributions usage current",
		},
		[]string{"account"},
	)
	cfDistributionsLimitedVec = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "total_cloudfront_distributions_usage_limited",
			Help: "Total cloudfront distributions usage limited",
		},
		[]string{"account"},
	)

	cfOAIInput      = cloudfront.ListCloudFrontOriginAccessIdentitiesInput{}
	cfOAICurrentVec = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "total_cloudfront_origin_access_identifies_usage_current",
			Help: "Total cloudfront origin access identifies usage current",
		},
		[]string{"account"},
	)
	cfOAILimitedVec = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "total_cloudfront_origin_access_identifies_usage_limited",
			Help: "Total cloudfront origin access identifies usage limited",
		},
		[]string{"account"},
	)
)
