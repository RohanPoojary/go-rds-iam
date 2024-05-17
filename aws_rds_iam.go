// Package go_rds_iam provides functionality to connect to AWS RDS instances using IAM authentication.
package go_rds_iam

import (
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/rds/rdsutils"
	"github.com/go-sql-driver/mysql"
	"github.com/lib/pq"
	"github.com/pkg/errors"
)

var (
	// mysqlRegex is a regular expression used to parse MySQL connection strings.
	mysqlRegex *regexp.Regexp
)

func init() {
	mysqlRegex = regexp.MustCompile(`^(?P<user>[^:]+):(?P<password>[^@]+)@tcp\((?P<host>[^:]+):(?P<port>\d+)\)\/(?P<dbname>[^?]+)`)
}

// ConnectionRequest represents a request to connect to an RDS instance.
type ConnectionRequest struct {
	RDSType ConnectionRDSType
	Region  string

	DBUser             string
	Hostname           string
	Port               int
	DBName             string
	SSLMode            string
	SSLCertificatePath string
}

func (cr *ConnectionRequest) sanitize() {
	if cr.RDSType == "" {
		cr.RDSType = "postgres"
	}

	if cr.Region == "" {
		cr.Region = "ap-south-1"
	}

	if cr.DBUser == "" {
		cr.DBUser = "postgres"
	}

	if cr.Hostname == "" {
		cr.Hostname = "localhost"
	}

	if cr.Port == 0 {
		if cr.RDSType == "postgres" {
			cr.Port = 5432
		} else if cr.RDSType == "mysql" {
			cr.Port = 3306
		}
	}

	if cr.DBName == "" {
		cr.DBName = "postgres"
	}
}

func createRDSConnectionString(sess *session.Session, request ConnectionRequest) (string, error) {
	creds := sess.Config.Credentials

	req := request
	req.sanitize()

	dbEndpoint := fmt.Sprintf("%s:%d", req.Hostname, req.Port)
	authToken, err := rdsutils.BuildAuthToken(dbEndpoint, req.Region, req.DBUser, creds)
	if err != nil {
		return "", errors.Wrap(err, "Unable to generate RDS auth token")
	}

	switch req.RDSType {
	case "postgres":
		connectionString := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s",
			req.Hostname, req.Port, req.DBUser, authToken, req.DBName,
		)

		if req.SSLMode != "" {
			connectionString += fmt.Sprintf(" sslmode=%s", req.SSLMode)
		}

		if req.SSLCertificatePath != "" {
			connectionString += fmt.Sprintf(" sslrootcert=%s", req.SSLCertificatePath)
		}

		return connectionString, nil
	case "mysql":
		return fmt.Sprintf("%s:%s@tcp(%s)/%s?allowCleartextPasswords=true",
			req.DBUser, authToken, dbEndpoint, req.DBName,
		), nil
	default:
		return "", errors.Errorf("Invalid RDSType: %v, Only 'postgres' or 'mysql' is supported", req.RDSType)
	}
}

func getPostgresValues(dsn string) map[string]string {
	if dsn == "" {
		return nil
	}

	result := make(map[string]string)
	dsnSplits := strings.Split(dsn, " ")
	for _, dsnSplit := range dsnSplits {
		valueSplit := strings.SplitN(dsnSplit, "=", 2)
		if len(valueSplit) != 2 {
			continue
		}

		result[valueSplit[0]] = valueSplit[1]
	}

	return result
}

func getMysqlValues(input string) map[string]string {
	if input == "" {
		return nil
	}

	// Extract named capture groups from the input string
	match := mysqlRegex.FindStringSubmatch(input)
	// Extract values from named capture groups
	result := make(map[string]string)
	for i, name := range mysqlRegex.SubexpNames() {
		if i != 0 && name != "" {
			result[name] = match[i]
		}
	}

	return result
}

func parseConnectionRequestFromDSN(rdsType ConnectionRDSType, dsn string) (ConnectionRequest, error) {
	var matchValues map[string]string

	if rdsType == "postgres" {
		matchValues = getPostgresValues(dsn)
	} else if rdsType == "mysql" {
		matchValues = getMysqlValues(dsn)
	} else {
		return ConnectionRequest{}, errors.Errorf("Invalid RDSType: %v, Only 'postgres' or 'mysql' is supported", rdsType)
	}

	if len(matchValues) == 0 {
		return ConnectionRequest{}, errors.Errorf("Invalid DSN: %v", dsn)
	}

	portString := matchValues["port"]
	port, _ := strconv.Atoi(portString)

	return ConnectionRequest{
		RDSType:            rdsType,
		DBUser:             matchValues["user"],
		Hostname:           matchValues["host"],
		Port:               port,
		DBName:             matchValues["dbname"],
		SSLMode:            matchValues["sslmode"],
		SSLCertificatePath: matchValues["sslrootcert"],
	}, nil
}

// GenericIAMDriver is a database driver that uses IAM authentication to connect to RDS instances.
type GenericIAMDriver struct {
	awsSession  *session.Session
	rdsType     ConnectionRDSType
	cachedCreds sync.Map
}

// Open opens a new database connection using IAM authentication.
func (d *GenericIAMDriver) Open(dsn string) (driver.Conn, error) {

	var iamDSN string
	var err error

	// Try making a connection with the cached IAM DSN
	if creds, ok := d.cachedCreds.Load(dsn); ok {
		iamDSN = creds.(string)
		if conn, err := d.open(iamDSN); err == nil {
			log.Println("Using cached IAM DSN for connection: ", dsn)
			return conn, nil
		}
	}

	iamDSN, err = d.generateNewIAMDSN(dsn)
	if err != nil {
		return nil, errors.Wrap(err, "generateNewIAMDSN")
	}

	d.cachedCreds.Store(dsn, iamDSN)
	log.Println("Created New DSN for connection: ", dsn)

	return d.open(iamDSN)
}

func (d *GenericIAMDriver) generateNewIAMDSN(dsn string) (string, error) {
	request, err := parseConnectionRequestFromDSN(d.rdsType, dsn)
	if err != nil {
		return "", errors.Wrap(err, "Error in Parsing DSN: "+dsn)
	}

	request.Region = *d.awsSession.Config.Region
	request.RDSType = ConnectionRDSType(d.rdsType)

	iamDSN, err := createRDSConnectionString(d.awsSession, request)
	return iamDSN, errors.Wrap(err, "createRDSConnectionString")
}

func (d *GenericIAMDriver) open(dsn string) (driver.Conn, error) {
	if d.rdsType == "postgres" {
		return pq.Driver{}.Open(dsn)
	} else {
		return mysql.MySQLDriver{}.Open(dsn)
	}
}

// RegisterAWSRDSIAMDrivers registers a new database driver for the given RDS type (e.g., "postgres" or "mysql")
// that uses IAM authentication to connect to RDS instances.
func RegisterAWSRDSIAMDrivers(sess *session.Session, rdsType ConnectionRDSType) string {
	driverName := "aws_" + string(rdsType) + "_iam"
	sql.Register(driverName, &GenericIAMDriver{
		awsSession:  sess,
		rdsType:     rdsType,
		cachedCreds: sync.Map{},
	})

	return driverName
}
