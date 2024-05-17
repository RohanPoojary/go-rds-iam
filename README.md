# AWS RDS IAM Authentication 

This Go package provides functionality to connect to AWS RDS instances using IAM authentication. It supports both PostgreSQL and MySQL databases and can be used in conjunction with the `database/sql` package and the `gorm` ORM library.

## Installation

To use this package, you need to have Go installed on your system. You can install it by following the official [Go installation guide](https://golang.org/doc/install).

Once you have Go installed, you can import this package into your Go project by adding the following line to your code:

```go
import "github.com/RohanPoojary/go-rds-iam"
```

## Usage

### Connecting to RDS using IAM authentication

To connect to an RDS instance using IAM authentication, you'll need to create an AWS session and register the IAM driver for the desired database type (PostgreSQL or MySQL). Here's an example:

```go

import (
    "database/sql"
    "github.com/RohanPoojary/go-rds-iam"
    "github.com/aws/aws-sdk-go/aws"
    "github.com/aws/aws-sdk-go/aws/credentials"
    "github.com/aws/aws-sdk-go/aws/session"
)

func main() {

    // Create an AWS session
    cfg := aws.Config{
        Region: &awsRegionName,
    }

    sess, err := session.NewSessionWithOptions(session.Options{
        Config:            cfg,
        SharedConfigState: session.SharedConfigEnable,
    })
    if err != nil {
        // Handle error
    }

    // Register the IAM driver for PostgreSQL
    driverName := go_rds_iam.RegisterAWSRDSIAMDrivers(sess, go_rds_iam.PostgresRDSType)

    // Connect to the RDS instance using the registered driver
    sqlDB, err := sql.Open(driverName, "your-rds-instance-dsn")
    if err != nil {
        // Handle error
    }

    // Use the database connection as needed
    // ...
}

```

### Using with GORM

To connect to GORM module, you can use below snippet along with above db initialisation to override default db connections:

```go

import (
    "gorm.io/driver/postgres"
    "database/sql"
)

func main() {

    var sqlDB *sql.DB

    // Initialise sqlDB with IAM driver as above
    // ...

    db, err := gorm.Open(postgres.New(postgres.Config{
        Conn: sqlDB,
    }), &gorm.Config{})

    if err != nil {
        // Handle error
    }
}

```

## Contributing

Contributions to this package are welcome! If you find a bug or want to add a new feature, please open an issue or submit a pull request on the project's GitHub repository.

## License

This package is licensed under the [MIT License](LICENSE).