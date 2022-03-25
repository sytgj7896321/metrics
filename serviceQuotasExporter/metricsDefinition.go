package main

import (
	"github.com/aws/aws-sdk-go-v2/service/acm"
	"github.com/aws/aws-sdk-go-v2/service/cloudfront"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/servicequotas"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	//failCount = prometheus.NewCounterVec(
	//	prometheus.CounterOpts{
	//		Name: "total_aws_api_call_failed_count",
	//		Help: "Total aws api call failed count",
	//	},
	//	[]string{"type"},
	//)

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

	s3Inout  = s3.ListBucketsInput{}
	s3Result = make(map[string]int)
	s3Vec    = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "total_buckets_usage_per_region",
			Help: "Total buckets usage per region",
		},
		[]string{"region"},
	)

	acmInput = acm.ListCertificatesInput{}
	acmVec   = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "total_certificates_usage_per_region",
			Help: "Total certificates usage per region",
		},
		[]string{"region"},
	)

	cloudFrontInput = cloudfront.ListDistributionsInput{}
	cloudFrontVec   = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "total_cloudfront_distributions_usage",
			Help: "Total cloudfront distributions usage",
		},
	)

	cloudFrontOAIInput = cloudfront.ListCloudFrontOriginAccessIdentitiesInput{}
	cloudFrontOAIVec   = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "total_cloudfront_origin_access_identify_usage",
			Help: "Total cloudfront origin access identify usage",
		},
	)
)
