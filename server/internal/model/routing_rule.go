package model

import "time"

const (
	RoutingRuleDomain          = "DOMAIN"
	RoutingRuleDomainSuffix    = "DOMAIN-SUFFIX"
	RoutingRuleDomainKeyword   = "DOMAIN-KEYWORD"
	RoutingRuleIPCIDR          = "IP-CIDR"
	RoutingRuleSourceIPCIDR    = "SRC-IP-CIDR"
	RoutingRuleDestinationPort = "DST-PORT"
	RoutingRuleNetwork         = "NETWORK"
	RoutingRuleInboundName     = "IN-NAME"
	RoutingRuleMatch           = "MATCH"
)

type RoutingRule struct {
	ID              int       `json:"id" gorm:"primaryKey;autoIncrement"`
	Name            string    `json:"name" gorm:"size:120;not null"`
	RuleType        string    `json:"rule_type" gorm:"size:32;index;not null"`
	Payload         string    `json:"payload" gorm:"size:512;not null"`
	TargetProfileID int       `json:"target_profile_id" gorm:"index;not null"`
	Enabled         bool      `json:"enabled" gorm:"index;not null;default:true"`
	SortOrder       int       `json:"sort_order" gorm:"index;not null;default:0"`
	CreatedAt       time.Time `json:"created_at" gorm:"index"`
	UpdatedAt       time.Time `json:"updated_at" gorm:"index"`
}

func ListRoutingRules() ([]*RoutingRule, error) {
	var items []*RoutingRule
	if err := DB.Order("sort_order asc, id asc").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}
