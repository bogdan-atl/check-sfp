pipeline {
    agent {
        node {
            label 'niva-local'
            retries 0
        }
    }
    environment {
        BINARY_NAME = 'sfp-parser'
        SERVICE_NAME = 'sfp-parser.service'
        TARGET_DIR = '/root/sfp'
    }
    stages {
        stage('Clean') {
            steps {
                script {
                    sh 'rm -f sfp-parser'
                }
            }
        }

        stage('Build') {
            steps {
                script {
                    sh '''
                        go mod tidy
                        CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o ${BINARY_NAME} .
                    '''
                    sh 'ls -la sfp-parser'
                }
            }
        }

        stage('Deploy') {
            steps {
                script {
                    sh '''
                        # Останавливаем сервис (если запущен)
                        sudo systemctl stop sfp-parser.service || true
            
           
                        sudo mkdir -p /root/sfp
                        sudo cp sfp-parser /root/sfp/sfp-parser
                        sudo chown root:root /root/sfp/sfp-parser
                        sudo chmod +x /root/sfp/sfp-parser
                    '''
                }
            }
        }

        stage('Restart Service') {
            steps {
                script {
                    sh '''
                        sudo systemctl stop ${SERVICE_NAME} || true
                        sudo systemctl daemon-reload
                        sudo systemctl start ${SERVICE_NAME}
                    '''
                }
            }
        }

        stage('Verify Service') {
            steps {
                script {
                    sh '''
                        sudo systemctl is-active --quiet ${SERVICE_NAME}
                        if [ $? -ne 0 ]; then
                            echo "❌ Сервис ${SERVICE_NAME} не запущен!"
                            sudo systemctl status ${SERVICE_NAME} --no-pager
                            exit 1
                        fi
                        echo "✅ OK ${SERVICE_NAME} ."
                    '''
                }
            }
        }
    }

    post {
        success {
            sh 'echo "Service deployed successfully at $(date)" | logger -t jenkins'
        }
        failure {
            sh 'echo "Deployment failed at $(date)" | logger -t jenkins'
        }
    }
}
